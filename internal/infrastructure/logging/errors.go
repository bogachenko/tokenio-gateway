package logging

import "errors"

var ErrInvalidLogLevel = errors.New("invalid log level")
var ErrInvalidLogFormat = errors.New("invalid log format")
var ErrBodyLoggingForbidden = errors.New("body logging is forbidden in production")
