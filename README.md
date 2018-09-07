
[![Build Status](https://travis-ci.org/coopernurse/caddy-awslambda.svg?branch=master)](https://travis-ci.org/coopernurse/caddy-awslambda) [![Coverage Status](https://coveralls.io/repos/github/coopernurse/caddy-awslambda/badge.svg?branch=master)](https://coveralls.io/github/coopernurse/caddy-awslambda?branch=master)

## Overview

`awslambda` is a Caddy plugin that gateways requests from Caddy to AWS Lambda functions.

awslambda proxies requests to AWS Lambda functions using the
[AWS Lambda Invoke](http://docs.aws.amazon.com/lambda/latest/dg/API_Invoke.html) operation.
It provides an alternative to AWS API Gateway and provides a simple way to declaratively proxy
requests to a set of Lambda functions without per-function configuration.

Given that AWS Lambda has no notion of request and response headers, this plugin defines a standard
JSON envelope format that encodes HTTP requests in a standard way, and expects the JSON returned from
the Lambda functions to conform to the response JSON envelope format.

*Contributors*: If you wish to contribute to this plugin, scroll to the bottom of this file
to the "Building" section for notes on how to build caddy locally with this plugin enabled.

## Examples

(1) Proxy all requests starting with /lambda/ to AWS Lambda, using env vars for AWS access keys and region:

```
awslambda /lambda/
```


(2) Proxy requests starting with `/api/` to AWS Lambda using the `us-west-2` region, for functions staring with `api-` but not ending with `-internal`. A qualifier is used to target the `prod` aliases for each function.

```
awslambda /api/ {
    aws_region  us-west-2
    qualifier   prod
    include     api-*
    exclude     *-internal
}
```

## Syntax

```
awslambda <path-prefix> {
    aws_access         aws access key value
    aws_secret         aws secret key value
    aws_region         aws region name
    qualifier          qualifier value
    include            included function names...
    exclude            excluded function names...
    name_prepend       string to prepend to function name
    name_append        string to append to function name
    single             name of a single lambda function to invoke
    strip_path_prefix  If true, path and function name are stripped from the path
}
```

*   **aws_access** is the AWS Access Key to use when invoking Lambda functions. If omitted, the AWS_ACCESS_KEY_ID env var is used.
*   **aws_secret** is the AWS Secret Key to use when invoking Lambda functions. If omitted, the AWS_SECRET_ACCESS_KEY env var is used.
*   **aws_region** is the AWS Region name to use (e.g. 'us-west-1'). If omitted, the AWS_REGION env var is used.
*   **qualifier** is the qualifier value to use when invoking Lambda functions. Typically this is set to a function version or alias name. If omitted, no qualifier will be passed on the AWS Invoke invocation.
*   **include** is an optional space separated list of function names to include. Prefix and suffix globs ('*') are supported. If omitted, any function name not excluded may be invoked.
*   **exclude** is an optional space separated list of function names to exclude. Prefix and suffix globs are supported.
*   **name_prepend** is an optional string to prepend to the function name parsed from the URL before invoking the Lambda.
*   **name_append** is an optional string to append to the function name parsed from the URL before invoking the Lambda.
*   **single** is an optional function name. If set, function name is not parsed from the URI path.
*   **strip_path_prefix** If 'true', path and function name is stripped from the path sent as request metadata to the Lambda function. (default=false)

Function names are parsed from the portion of request path following the path-prefix in the
directive based on this convention: `[path-prefix]/[function-name]/[extra-path-info]` unless `single` attribute is set.

For example, given a directive `awslambda /lambda/`, requests to `/lambda/hello-world` and `/lambda/hello-world/abc`
would each invoke the AWS Lambda function named `hello-world`.

The `include` and `exclude` globs are simple wildcards, not regular expressions.
For example, `include foo*` would match `food` and `footer` but not `buffoon`, while
`include *foo*` would match all three.

`include` and `exclude` rules are run before `name_prepend` and `name_append` are applied and
are run against the parsed function name, not the entire URL path.

If you adopt a simple naming convention for your Lambda functions, these rules can be used to
group access to a set of Lambdas under a single URL path prefix.

`name_prepend` and `name_append` allow for shorter names in URLs and works well with tools such
as Apex, which prepend the project name to all Lambda functions. For example, given an URL path
of `/api/foo` with a `name_prepend` of `acme-api-`, the plugin will try to invoke the function
named `acme-api-foo`.

## Writing Lambdas

See [Lambda Functions](/docs/awslambda-functions) for details on the JSON request and reply
envelope formats. Lambda functions that comply with this format may set arbitrary HTTP response
status codes and headers.

All examples in this document use the `node-4.3` AWS Lambda runtime.

### Examples

Consider this `Caddyfile`:

```
awslambda /caddy/ {
   aws_access  redacted
   aws_secret  redacted
   aws_region  us-west-2
   include     caddy-*
}
```

And this Lambda function, named `caddy-echo`:

```javascript
'use strict';
exports.handler = (event, context, callback) => {
    callback(null, event);
};
```

When we request it via `curl` we receive the following response, which reflects the
request envelope Caddy sent to the lambda function:


```
$ curl -s -X POST -d 'hello' http://localhost:2015/caddy/caddy-echo | jq .
{
  "type": "HTTPJSON-REQ",
  "meta": {
    "method": "POST",
    "path": "/caddy/caddy-echo",
    "query": "",
    "host": "localhost:2020",
    "proto": "HTTP/1.1",
    "headers": {
      "accept": [
        "*/*"
      ],
      "content-length": [
        "5"
      ],
      "content-type": [
        "application/x-www-form-urlencoded"
      ],
      "user-agent": [
        "curl/7.43.0"
      ]
    }
  },
  "body": "hello"
}
```

The request envelope format is described in detail below, but there are three top level fields:

* `type` - always set to `HTTPJSON-REQ`
* `meta` - JSON object containing HTTP request metadata such as the request method and headers
* `body` - HTTP request body (if provided)

Since our Lambda function didn't respond using the reply envelope, the raw reply was sent
to the HTTP client and the `Content-Type` header was set to `application/json` automatically.

Let's write a 2nd Lambda function that uses the request metadata and sends a reply using the
envelope format.

Lambda function name: `caddy-echo-html`

```javascript
'use strict';
exports.handler = (event, context, callback) => {
    var html, reply;
    html = '<html><head><title>Caddy Echo</title></head>' +
           '<body><h1>Request:</h1>' +
           '<pre>' + JSON.stringify(event, null, 2) +
           '</pre></body></html>';
    reply = {
        'type': 'HTTPJSON-REP',
        'meta': {
            'status': 200,
            'headers': {
                'Content-Type': [ 'text/html' ]
            }
        },
        body: html
    };
    callback(null, reply);
};
```

If we request `http://localhost:2015/caddy/caddy-echo-html` in a desktop web browser, the HTML
formatted reply is displayed with a pretty-printed version of the request inside `<pre>` tags.

In a final example we'll send a redirect using a 302 HTTP response status.

Lambda function name: `caddy-redirect`

```javascript
'use strict';
exports.handler = (event, context, callback) => {
    var redirectUrl, reply;
    redirectUrl = 'https://caddyserver.com/'
    reply = {
        'type': 'HTTPJSON-REP',
        'meta': {
            'status': 302,
            'headers': {
                'Location': [ redirectUrl ]
            }
        },
        body: 'Page has moved to: ' + redirectUrl
    };
    callback(null, reply);
};
```

If we request `http://localhost:2015/caddy/caddy-redirect` we are redirected to the Caddy home page.


### Request envelope

The request payload sent from Caddy to the AWS Lambda function is a JSON object with the following fields:

* `type` - always the string literal `HTTPJSON-REQ`
* `body` - the request body, or an empty string if no body is provided.
* `meta` - a JSON object with the following fields:
  * `method` - HTTP request method (e.g. `GET` or `POST`)
  * `path` - URI path without query string
  * `query` - Raw query string (without '?')
  * `host` - Host client request was made to. May be of the form host:port
  * `proto` - Protocol used by the client
  * `headers` - a JSON object of HTTP headers sent by the client. Keys will be lower case. Values will be string arrays.

### Reply envelope

AWS Lambda functions should return a JSON object with the following fields:

* `type` - always the string literal `HTTPJSON-REP`
* `body` - response body
* `meta` - optional response metadata. If provided, must be a JSON object with these fields:
  * `status` - HTTP status code (e.g. 200)
  * `headers` - a JSON object of HTTP headers. Values **must** be string arrays.

If `meta` is not provided, a 200 status will be returned along with a `Content-Type: application/json` header.

### Gotchas

* Request and reply header values must be **string arrays**. For example:

```javascript
// Valid
var reply = {
    'type': 'HTTPJSON-REP',
    'meta': {
        'headers': {
            'content-type': [ 'text/html' ]
        }
    }
};

// Invalid
var reply = {
    'type': 'HTTPJSON-REP',
    'meta': {
        'headers': {
            'content-type': 'text/html'
        }
    }
};
```

* Reply must have a top level `'type': 'HTTPJSON-REP'` field. The rationale is that since
all Lambda responses must be JSON we need a way to detect the presence of the envelope. Without
this field, the raw reply JSON will be sent back to the client unmodified.
    
## Building

If you want to modify the plugin and test your changes locally, follow these steps to
recompile caddy with the plugin installed:

```bash
go get github.com/mholt/caddy/caddy
cd $GOPATH/src/github.com/mholt/caddy/caddy
```

Edit `caddymain/run.go` and add this to the import section:

```
_ "github.com/coopernurse/caddy-awslambda"
```

Then run the `build.go` step:

```bash
cd $GOPATH/src/github.com/mholt/caddy/caddy
go run build.go
```

That will create a `caddy` binary in the current directory linking in the
awslambda plugin with whatever local changes you've made.

Verify that the plugin is installed:

```bash
./caddy -plugins | grep aws

# you should see:
  http.awslambda
```

These instructions are based on these notes:
https://github.com/mholt/caddy/wiki/Plugging-in-Plugins-Yourself
