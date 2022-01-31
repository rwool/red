package main

import (
	"bytes"
	"context"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

func testSetup(t *testing.T) *ActivityGenerator {
	t.Parallel()
	ag, err := NewActivityGenerator(ActivityGeneratorConfig{Log: newLogger(t)})
	require.NoError(t, err)
	return ag
}

func TestFile(t *testing.T) {
	t.Parallel()
	td, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, os.RemoveAll(td))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	t.Run("default ActivityGenerator", func(t *testing.T) {
		var conf ActivityGeneratorConfig
		_, err := NewActivityGenerator(conf)
		require.NoError(t, err)
	})
	t.Run("create only", func(t *testing.T) {
		ag := testSetup(t)
		f, err := ag.CreateFile(filepath.Join(td, "create only"))
		assert.NoError(t, err)
		assert.NoError(t, f.Close())
	})
	t.Run("create modify delete", func(t *testing.T) {
		ag := testSetup(t)
		f, err := ag.CreateFile(filepath.Join(td, "cmd"))
		require.NoError(t, err)
		defer func() { assert.NoError(t, f.Close()) }()
		require.NoError(t, ag.ModifyFile(ctx, f, bytes.NewBufferString("hello")))
		assert.NoError(t, ag.DeleteFile(f.Name()))
	})
	t.Run("delete without create", func(t *testing.T) {
		ag := testSetup(t)
		assert.ErrorIs(t, ag.DeleteFile(filepath.Join(td, "does not exist")), os.ErrNotExist)
	})
}

// bufferServer accepts a single incoming connection and writes all bytes seen
// to a buffer. The connection is closed and the buffer is safe to read from
// once done is closed.
func bufferServer(t *testing.T) (addr string, _ *bytes.Buffer, done <-chan struct{}) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0, // Randomly chosen open port.
	})
	require.NoError(t, err)
	var buf bytes.Buffer
	doneC := make(chan struct{})
	go func() {
		defer close(doneC)
		conn, err := l.Accept()
		assert.NoError(t, err)
		assert.NoError(t, l.Close())
		_, err = io.Copy(&buf, conn)
		assert.NoError(t, err)
		assert.NoError(t, conn.Close())
	}()
	return l.Addr().String(), &buf, doneC
}

func TestNetworkConnection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	t.Run("send one byte", func(t *testing.T) {
		ag := testSetup(t)
		server, buf, doneC := bufferServer(t)
		err := ag.ConnectAndTransmit(ctx, server, bytes.NewBufferString("a"))
		assert.NoError(t, err)
		select {
		case <-ctx.Done():
			t.Fatalf("timeout: %v", ctx.Err())
		case <-doneC:
			assert.Equal(t, "a", buf.String())
		}
	})
	t.Run("send zero bytes", func(t *testing.T) {
		ag := testSetup(t)
		server, buf, doneC := bufferServer(t)
		err := ag.ConnectAndTransmit(ctx, server, bytes.NewBufferString(""))
		assert.NoError(t, err)
		select {
		case <-ctx.Done():
			t.Fatalf("timeout: %v", ctx.Err())
		case <-doneC:
			assert.Equal(t, "", buf.String())
		}
	})
	t.Run("error sending to unopen port", func(t *testing.T) {
		ag := testSetup(t)
		err := ag.ConnectAndTransmit(ctx, "127.0.0.1:1", bytes.NewBufferString(""))
		assert.Error(t, err)
	})
}

func TestRunProcess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	t.Run("runs true", func(t *testing.T) {
		t.Parallel()
		ag, err := NewActivityGenerator(ActivityGeneratorConfig{Log: newLogger(t)})
		require.NoError(t, err)
		assert.NoError(t, ag.RunProcess(ctx, "true", nil))
	})
	t.Run("run non-existent", func(t *testing.T) {
		t.Parallel()
		ag, err := NewActivityGenerator(ActivityGeneratorConfig{Log: newLogger(t)})
		require.NoError(t, err)
		assert.ErrorIs(t, ag.RunProcess(ctx, filepath.Join(".", "does not exist"), nil), exec.ErrNotFound)
	})
	t.Run("run no-exec", func(t *testing.T) {
		t.Parallel()
		ag, err := NewActivityGenerator(ActivityGeneratorConfig{Log: newLogger(t)})
		require.NoError(t, err)
		f, err := os.CreateTemp("", "")
		require.NoError(t, err)
		defer func() {
			assert.NoError(t, f.Close())
			assert.NoError(t, os.Remove(f.Name()))
		}()
		require.NoError(t, f.Chmod(0600)) // Read and write. No exec.
		assert.ErrorIs(t, ag.RunProcess(ctx, f.Name(), nil), os.ErrPermission)
	})
}
