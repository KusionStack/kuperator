/*
Copyright 2023 The KusionStack Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"

	"kusionstack.io/kafed/apis"
)

var TestMode = false

func init() {
	if os.Getenv(apis.EnvTestMode) == "true" {
		TestMode = true
	}
}

type InfoLogger struct {
	V          int
	rootLogger *Logger
}

func (l *InfoLogger) Infof(format string, args ...interface{}) {
	if TestMode {
		klog.Infof(format, args...)
	} else {
		l.rootLogger.logger.V(l.V).Info(fmt.Sprintf(format, args...))
	}
}

type Logger struct {
	logger logr.Logger
}

func New(logger logr.Logger) *Logger {
	return &Logger{logger}
}

func (l *Logger) V(v int) *InfoLogger {
	// zap has no verbosity, always log info as 0 level
	return &InfoLogger{
		V:          0,
		rootLogger: l,
	}
}

func (l *Logger) Warningf(format string, args ...interface{}) {
	if TestMode {
		klog.Warningf(format, args...)
	} else {
		l.logger.Info(fmt.Sprintf(format, args...))
	}
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	if TestMode {
		klog.Errorf(format, args...)
	} else {
		l.logger.Error(fmt.Errorf(format, args...), "")
	}
}

func (l *Logger) Infof(format string, args ...interface{}) {
	if TestMode {
		klog.Infof(format, args...)
	} else {
		l.logger.Info(fmt.Sprintf(format, args...))
	}
}

func (l *Logger) With(kind, item string) *Logger {
	logger := l.logger.WithValues(kind, item)
	return &Logger{logger}
}
