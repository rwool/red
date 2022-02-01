package main

import (
	"context"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
	"time"
)

// testWriter is used to scope log output to tests.
type testWriter struct {
	t *testing.T
}

// Write allows for writes to be logged to a specific test.
func (t *testWriter) Write(p []byte) (n int, err error) {
	t.t.Helper()
	// This prints the wrong line information.
	t.t.Logf("%s", p)
	return len(p), nil
}

func newLogger(t *testing.T) *logrus.Logger {
	tw := &testWriter{t: t}
	l := logrus.New()
	l.Formatter = &logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "timestamp",
		},
		PrettyPrint: true, // For test output readability.
	}
	l.Out = tw
	return l
}

func TestLocalhostTCPConnect(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var buf strings.Builder
	sent, err := LocalhostTCPConnect(
		ctx,
		newLogger(t).WithField("test", t.Name()),
		strings.NewReader("hello"),
		&buf,
	)
	assert.NoError(t, err)
	assert.Equal(t, int64(len("hello")), sent)
	assert.Equal(t, "hello", buf.String())
}

func TestRunActivities(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := RunActivities(ctx, newLogger(t), "", ".txt", nil)
	assert.NoError(t, err)
}
