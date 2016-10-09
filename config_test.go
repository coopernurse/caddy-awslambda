package awslambda

import (
	"bytes"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/mholt/caddy"
)

func TestAcceptsFunction(t *testing.T) {
	c := Config{
		Include: []string{
			"test*", "hello-world",
		},
		Exclude: []string{
			"*cats*", "fish",
		},
	}

	for i, test := range []struct {
		name     string
		expected bool
	}{
		{"", false},
		{"test", true},
		{"testSomething", true},
		{"test-cats", false},
		{"test-fishy-stuff", true},
		{"fish", false},
		{"test_dog", true},
		{"hello-world", true},
		{"hello-world-2", false},
	} {
		actual := c.AcceptsFunction(test.name)
		if actual != test.expected {
			t.Errorf("\nTest %d - name: %s\nExpected: %v\n  Actual: %v",
				i, test.name, test.expected, actual)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	for i, test := range []struct {
		candidate string
		rule      string
		expected  bool
	}{
		{"hello", "hello", true},
		{"hello", "ello", false},
		{"hello", "*ello", true},
		{"hello", "hel*", true},
	} {
		actual := matchGlob(test.candidate, test.rule)
		if actual != test.expected {
			t.Errorf("\nTest %d - candidate: %s    rule: %s\nExpected: %v\n  Actual: %v",
				i, test.candidate, test.rule, test.expected, actual)
		}
	}
}

func TestToAwsConfigStaticCreds(t *testing.T) {
	c := &Config{
		AwsAccess: "a-key",
		AwsSecret: "secret",
	}
	expected := credentials.NewStaticCredentials("a-key", "secret", "")
	actual := c.ToAwsConfig()
	if !reflect.DeepEqual(expected, actual.Credentials) {
		t.Errorf("\nExpected: %v\n  Actual: %v", expected, actual.Credentials)
	}
}

func TestToAwsConfigStaticRegion(t *testing.T) {
	c := &Config{
		AwsRegion: "us-west-2",
	}
	expected := aws.NewConfig()
	actual := c.ToAwsConfig()
	if c.AwsRegion != *actual.Region {
		t.Errorf("\nExpected: %v\n  Actual: %v", c.AwsRegion, *actual.Region)
	}
	if !reflect.DeepEqual(expected.Credentials, actual.Credentials) {
		t.Errorf("\nExpected: %v\n  Actual: %v", expected.Credentials, actual.Credentials)
	}
}

func TestToAwsConfigDefaults(t *testing.T) {
	c := &Config{}
	expected := aws.NewConfig()
	actual := c.ToAwsConfig()
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("\nExpected: %v\n  Actual: %v", expected, actual.Credentials)
	}
}

func TestParseConfigs(t *testing.T) {
	for i, test := range []struct {
		input    string
		expected []*Config
	}{
		{"awslambda /foo/", []*Config{&Config{
			Path:    "/foo/",
			Include: []string{},
			Exclude: []string{},
		}}},
		{`awslambda /blah/ {
    aws_access my-access
    aws_secret my-secret
    aws_region us-west-1
    qualifier  prod
    include    foo*  some-other
    exclude    *blah*
    name_prepend   apex-foo_
    name_append    _suffix_here
    single             my-single-func
    strip_path_prefix  on
}`,
			[]*Config{
				&Config{
					Path:            "/blah/",
					AwsAccess:       "my-access",
					AwsSecret:       "my-secret",
					AwsRegion:       "us-west-1",
					Qualifier:       "prod",
					Include:         []string{"foo*", "some-other"},
					Exclude:         []string{"*blah*"},
					NamePrepend:     "apex-foo_",
					NameAppend:      "_suffix_here",
					Single:          "my-single-func",
					StripPathPrefix: true,
				},
			},
		},
		{`awslambda /first/ {
    aws_region us-west-2
    qualifier  dev
    exclude    foo
}
awslambda /second/path/ {
    aws_region us-east-1
    include one two three*
}`,
			[]*Config{
				&Config{
					Path:      "/first/",
					AwsRegion: "us-west-2",
					Qualifier: "dev",
					Include:   []string{},
					Exclude:   []string{"foo"},
				},
				&Config{
					Path:      "/second/path/",
					AwsRegion: "us-east-1",
					Include:   []string{"one", "two", "three*"},
					Exclude:   []string{},
				},
			},
		},
	} {
		controller := caddy.NewTestController("http", test.input)
		actual, err := ParseConfigs(controller)
		if err != nil {
			t.Errorf("ParseConfigs return err: %v", err)
		}
		for i := range actual {
			actual[i].invoker = nil
		}
		eqOrErr(test.expected, actual, i, t)
	}
}

func TestParseFunction(t *testing.T) {
	for i, test := range []struct {
		path     string
		expected string
	}{
		{"/foo/bar", "bar"},
		{"/foo/bar/baz", "bar"},
		{"/foo", ""},
		{"/foo/", ""},
		{"/foo/bar?a=b", "bar"},
		{"/foo/bar#anchor-here", "bar"},
		{"/foo/bar?a=/blah#anchor-here", "bar"},
		{"/foo/bar/baz?a=/blah#anchor-here", "bar"},
	} {
		c := Config{Path: "/foo/"}
		actual := c.ParseFunction(test.path)
		if actual != test.expected {
			t.Errorf("\nTest %d\nExpected: %s\n  Actual: %s", i, test.expected, actual)
		}
	}
}

func TestMaybeToInvokeInput(t *testing.T) {
	r1 := mustNewRequest("PUT", "/api/user", bytes.NewBufferString("hello world"))
	r2 := mustNewRequest("PUT", "/api/user", bytes.NewBufferString("hello world"))

	// expect a non-nil input
	c := Config{
		Path:        "/api/",
		NamePrepend: "before-",
		NameAppend:  "-after",
		Qualifier:   "prod",
	}
	input, err := c.MaybeToInvokeInput(r1)
	if err != nil {
		t.Fatalf("MaybeToInvokeInput returned err: %v", err)
	}
	if input == nil {
		t.Fatalf("MaybeToInvokeInput returned nil input")
	}
	funcName := "before-user-after"
	req, err := NewRequest(r2)
	if err != nil {
		t.Fatalf("NewRequest returned err: %v", err)
	}
	expected := lambda.InvokeInput{
		FunctionName: &funcName,
		Qualifier:    &c.Qualifier,
		Payload:      marshalJSON(req),
	}
	eqOrErr(expected, *input, 0, t)

	// expect a nil input since include rule doesn't match
	c.Include = []string{"*blah*"}
	input, err = c.MaybeToInvokeInput(r1)
	if err != nil || input != nil {
		t.Fatalf("MaybeToInvokeInput returned err or non-nil input: input=%v  err=%v", input, err)
	}
}

func TestSingleFunction(t *testing.T) {
	r1 := mustNewRequest("PUT", "/api/user", bytes.NewBufferString("hello world"))

	// expect a non-nil input
	c := Config{
		Single: "single-func",

		// ignored:
		Exclude: []string{"single"},
		Include: []string{"foo"},
	}
	input, err := c.MaybeToInvokeInput(r1)
	if err != nil {
		t.Fatalf("MaybeToInvokeInput returned err: %v", err)
	}
	if c.Single != *input.FunctionName {
		t.Errorf("FunctionName wrong: %s != %s", c.Single, input.FunctionName)
	}
}

func TestStripPathPrefix(t *testing.T) {
	c := Config{
		Path:   "/api/",
		Single: "single-func",
	}

	for i, test := range []struct {
		reqPath  string
		funcName string
		isSingle bool
		expected string
	}{
		{"/api/foo", "foo", false, "/"},
		{"/api/blahstuff/other/things", "no-match", false, "/api/blahstuff/other/things"},
		{"/api/foo", "foo", true, "/foo"},
		{"/other/foo", "foo", false, "/other/foo"},
		{"/other/foo", "foo", true, "/other/foo"},
	} {
		c.Single = ""
		if test.isSingle {
			c.Single = "single-func"
		}

		actual := c.stripPathPrefix(test.reqPath, test.funcName)
		if actual != test.expected {
			t.Errorf("Test %d failed:\nExpected: %s\n  Actual: %s", i, test.expected, actual)
		}
	}
}

func mustNewRequest(method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		panic(err)
	}
	return req
}
