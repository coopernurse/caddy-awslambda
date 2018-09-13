package awslambda

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyhttp/httpserver"
)

// Config specifies configuration for a single awslambda block
type Config struct {
	// Path this config block maps to
	Path string
	// AWS Access Key. If omitted, AWS_ACCESS_KEY_ID env var is used.
	AwsAccess string
	// AWS Secret Key. If omitted, AWS_SECRET_ACCESS_KEY env var is used.
	AwsSecret string
	// AWS Region. If omitted, AWS_REGION env var is used.
	AwsRegion string
	// Optional qualifier to use on Invoke requests.
	// This can be used to pin a configuration to a particular alias (e.g. 'prod' or 'dev')
	Qualifier string
	// Function name include rules. Prefix and suffix '*' globs are supported.
	// Functions matching *any* of these rules will be proxied.
	// If Include is empty, all function names will be allowed (unless explicitly excluded).
	Include []string
	// Function name exclude rules. Prefix and suffix '*" globs are supported.
	// Functions matching *any* of these rules will be excluded, and not proxied.
	// If Exclude is empty, no exclude rules will be applied.
	Exclude []string
	// Optional strings to prepend or append to the parsed function name from the URL
	// before invoking the lambda. These are applied after the Include/Exclude rules are run
	NamePrepend string
	NameAppend  string

	// If set, all requests to this path will invoke this function.
	// The function name will not be parsed from the URL.
	// This is useful for cases where you are multiplexing requests inside
	// the lambda function itself.
	//
	// Note: If set, Include and Exclude will be ignored.
	//
	Single string

	// If true, the Path field and function name will be removed from the
	// RequestMeta.Path sent to the lambda function.  If Single is set,
	// only the Path will be removed.
	//
	// For example, given: awslambda /api/ and a request to: /api/hello/foo
	// the RequestMeta.Path would be /foo
	StripPathPrefix bool

	// headers to set in the upstream "headers" array - caddy placeholders work here
	UpstreamHeaders map[string][]string

	invoker Invoker
}

// AcceptsFunction tests whether the given function name is supported for
// this configuration by applying the Include and Exclude rules.
//
// Some additional lightweight sanity tests are also performed.  For example,
// empty strings and names containing periods (prohibited by AWS Lambda) will
// return false, but there is no attempt to ensure that all AWS Lambda naming
// rules are validated.  That is, some invalid names could be passed through.
//
func (c *Config) AcceptsFunction(name string) bool {
	if name == "" || strings.Index(name, ".") >= 0 {
		return false
	}

	if len(c.Include) > 0 {
		found := false
		for _, k := range c.Include {
			if matchGlob(name, k) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, k := range c.Exclude {
		if matchGlob(name, k) {
			return false
		}
	}

	return true
}

// ToAwsConfig returns a new *aws.Config instance using the AWS related values on Config.
// If AwsRegion is empty, the AWS_REGION env var is used.
// If AwsAccess is empty, the AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY env vars are used.
func (c *Config) ToAwsConfig() *aws.Config {
	awsConf := aws.NewConfig()
	if c.AwsRegion != "" {
		awsConf.WithRegion(c.AwsRegion)
	}
	if c.AwsAccess != "" {
		awsConf.WithCredentials(credentials.NewStaticCredentials(
			c.AwsAccess, c.AwsSecret, "",
		))
	}
	return awsConf
}

// ParseFunction returns the fragment of path immediately after the
// config path, excluding string and named anchors.
//
// For example, given a path of '/lambda/my-func/pathparam?a=/foo',
// ParseFunction returns 'my-func'
func (c *Config) ParseFunction(path string) string {
	path = strings.TrimPrefix(path, c.Path)
	pos := strings.Index(path, "?")
	if pos > -1 {
		path = path[:pos]
	}
	pos = strings.Index(path, "#")
	if pos > -1 {
		path = path[:pos]
	}

	return strings.Split(path, "/")[0]
}

// MaybeToInvokeInput returns a new InvokeInput instanced based on the  HTTP request.
// If the function name parsed from the r.URL.Path doesn't comply with the Config's
// include/exclude rules, then nil, nil is returned.
// Otherwise an InvokeInput is returned with all fields populated based on the
// http.Request, and the NameAppend and NamePrepend rules applied (if any).
func (c *Config) MaybeToInvokeInput(r *http.Request) (*lambda.InvokeInput, error) {
	// Verify that parsed function name is allowed based on Config rules
	funcName := c.Single
	if funcName == "" {
		funcName = c.ParseFunction(r.URL.Path)
		if !c.AcceptsFunction(funcName) {
			return nil, nil
		}
	}

	req, err := NewRequest(r)
	if err != nil {
		return nil, err
	}
	if c.StripPathPrefix && req.Meta != nil {
		req.Meta.Path = c.stripPathPrefix(req.Meta.Path, funcName)
	}

	if len(c.UpstreamHeaders) > 0 {
		// inject upstream headers defined with the header_upstream directive into req.Meta.Headers
		// uses caddy's integrated replacer for placeholder replacement (https://caddyserver.com/docs/placeholders)
		replInt := r.Context().Value(httpserver.ReplacerCtxKey)
		replacer := replInt.(httpserver.Replacer)
		for k, v := range c.UpstreamHeaders {
			newValue := make([]string, len(v))
			for i, v := range v {
				newValue[i] = replacer.Replace(v)
			}
			req.Meta.Headers[strings.ToLower(k)] = newValue
		}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if c.NamePrepend != "" {
		funcName = c.NamePrepend + funcName
	}
	if c.NameAppend != "" {
		funcName = funcName + c.NameAppend
	}

	input := &lambda.InvokeInput{
		FunctionName: &funcName,
		Payload:      payload,
	}
	if c.Qualifier != "" {
		input.Qualifier = &c.Qualifier
	}
	return input, nil
}

func (c *Config) initLambdaClient() error {
	sess, err := session.NewSession(c.ToAwsConfig())
	if err != nil {
		return err
	}
	c.invoker = lambda.New(sess)
	return nil
}

func (c *Config) stripPathPrefix(reqPath, funcName string) string {
	prefix := c.Path
	if c.Single == "" {
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		prefix += funcName
	}

	if strings.HasPrefix(reqPath, prefix) {
		reqPath = reqPath[len(prefix):]
		if !strings.HasPrefix(reqPath, "/") {
			reqPath = "/" + reqPath
		}
	}
	return reqPath
}

// ParseConfigs parses a Caddy awslambda config block into a Config struct.
func ParseConfigs(c *caddy.Controller) ([]*Config, error) {
	var configs []*Config
	var conf *Config
	last := ""

	for c.Next() {
		val := c.Val()
		lastTmp := last
		last = ""
		switch lastTmp {
		case "awslambda":
			conf = &Config{
				Path:    val,
				Include: []string{},
				Exclude: []string{},
			}
			configs = append(configs, conf)
		case "aws_access":
			conf.AwsAccess = val
		case "aws_secret":
			conf.AwsSecret = val
		case "aws_region":
			conf.AwsRegion = val
		case "qualifier":
			conf.Qualifier = val
		case "name_prepend":
			conf.NamePrepend = val
		case "name_append":
			conf.NameAppend = val
		case "single":
			conf.Single = val
		case "strip_path_prefix":
			conf.StripPathPrefix = toBool(val)
		case "include":
			conf.Include = append(conf.Include, val)
			conf.Include = append(conf.Include, c.RemainingArgs()...)
		case "exclude":
			conf.Exclude = append(conf.Exclude, val)
			conf.Exclude = append(conf.Exclude, c.RemainingArgs()...)
		case "header_upstream":
			if conf.UpstreamHeaders == nil {
				conf.UpstreamHeaders = make(map[string][]string)
			}
			value := strings.Join(c.RemainingArgs(), " ")
			conf.UpstreamHeaders[val] = []string{value}
		default:
			last = val
		}
	}

	for _, conf := range configs {
		err := conf.initLambdaClient()
		if err != nil {
			return nil, err
		}
	}

	return configs, nil
}

// toBool treats any of the following as true: 1, yes, y, on, true
// otherwise returns false
func toBool(s string) bool {
	s = strings.ToLower(s)
	if s == "1" || s == "y" || s == "yes" || s == "true" || s == "on" {
		return true
	}
	return false
}

// matchGlob returns true if string s matches the rule.
// Simple prefix and suffix wildcards are supported with '*'.
// For example, string 'hello' matches rules: 'hello', 'hel*', '*llo', '*ell*'
func matchGlob(s, rule string) bool {
	if s == rule {
		return true
	}

	if strings.HasPrefix(rule, "*") {
		if strings.HasSuffix(rule, "*") {
			rule = rule[1 : len(rule)-1]
			return strings.Index(s, rule) >= 0
		}
		return strings.HasSuffix(s, rule[1:])
	} else if strings.HasSuffix(rule, "*") {
		return strings.HasPrefix(s, rule[0:len(rule)-1])
	} else {
		return false
	}
}
