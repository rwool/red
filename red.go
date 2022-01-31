package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// ActivityGeneratorConfig contains the configuration for an ActivityGenerator.
type ActivityGeneratorConfig struct {
	Log *logrus.Logger
}

// ActivityGenerator is able to generate various types of activity.
type ActivityGenerator struct {
	Log *logrus.Entry
}

// NewActivityGenerator creates an activity generator.
func NewActivityGenerator(conf ActivityGeneratorConfig) (*ActivityGenerator, error) {
	log := conf.Log
	if log == nil {
		log = logrus.New()
		log.Formatter = &logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime: "timestamp",
			},
		}
	}
	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("unable to get user information: %w", err)
	}
	return &ActivityGenerator{
		// Add the fields common to all types of activity.
		Log: log.WithFields(logrus.Fields{
			"username":     u.Username,
			"process name": os.Args[0],
			// May be difficult to parse this if there are spaces in an
			// argument.
			"process command line": strings.Join(os.Args, " "),
			"process ID":           os.Getpid(),
		}),
	}, nil
}

// RunProcess runs a process with the executable specified by path.
//
// This function blocks until the program returns.
func (ag *ActivityGenerator) RunProcess(ctx context.Context, path string, args []string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Start()
	errLog(ag.Log, err, "process start")
	if err != nil {
		return fmt.Errorf("error starting command: %w", err)
	}
	// May not actually be an error for a command to return non-zero status
	// code.
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("error running command: %w", err)
	}
	return nil
}

// CreateFile creates a file at the given path.
func (ag *ActivityGenerator) CreateFile(path string) (*os.File, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("unable to get absolute path from %q: %w", path, err)
	}
	file, err := os.Create(absPath)
	l := ag.Log.WithFields(logrus.Fields{
		"file activity": "create",
		"file path":     absPath,
	})
	errLog(l, err, "create file")
	if err != nil {
		return nil, fmt.Errorf("unable to create file: %w", err)
	}
	return file, nil
}

// ModifyFile modifies file by writing the data from reader to it.
func (ag *ActivityGenerator) ModifyFile(ctx context.Context, file *os.File, reader io.Reader) error {
	// Add filepath logging.
	if file == nil {
		return errors.New("error attempting to modify nil file")
	}
	absPath, err := filepath.Abs(file.Name())
	if err != nil {
		return fmt.Errorf("unable to get absolute path from %q: %w", file.Name(), err)
	}

	// Set write deadline from ctx, if applicable.
	ctxDl, ctxDlOk := ctx.Deadline()
	if ctxDlOk {
		if err := file.SetWriteDeadline(ctxDl); err != nil && !errors.Is(err, os.ErrNoDeadline) {
			return fmt.Errorf("unable to set write deadline: %w", err)
		}
	}

	_, err = io.Copy(file, reader)
	l := ag.Log.WithFields(logrus.Fields{
		"file activity": "modify",
		"file path":     absPath,
	})
	errLog(l, err, "modify file")
	if err != nil {
		return errors.New("error attempting to modify file")
	}
	return nil
}

// DeleteFile deletes the file at path.
func (ag *ActivityGenerator) DeleteFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("unable to get absolute path from %q: %w", path, err)
	}
	l := ag.Log.WithFields(logrus.Fields{
		"file activity": "delete",
		"file path":     absPath,
	})
	err = os.Remove(path)
	errLog(l, err, "delete file")
	if err != nil {
		return fmt.Errorf("error attempting to delete file: %w", err)
	}
	return nil
}

// ConnectAndTransmit connects to addr and transmits the data in payload over a
// TCP connection.
func (ag *ActivityGenerator) ConnectAndTransmit(ctx context.Context, addr string, payload io.Reader) error {
	if payload == nil {
		return errors.New("invalid nil payload io.Reader")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("unable to connect to address %q: %w", addr, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			ag.Log.WithError(err).Error("close client network connection")
		}
	}()
	var (
		localAddr  = conn.LocalAddr()
		remoteAddr = conn.RemoteAddr()
		sent       int64
		l          = ag.Log.WithFields(logrus.Fields{
			"destination address": remoteAddr.String(),
			"source address":      localAddr.String(),
			"protocol":            localAddr.Network(),
		})
	)
	sent, err = io.Copy(conn, payload)
	errLog(l.WithField("data sent (bytes)", sent), err, "data transmission")
	if err != nil {
		return fmt.Errorf("error transmitting data: %w", err)
	}
	return nil
}

func errLog(l *logrus.Entry, e error, msg string) {
	if e != nil {
		l.WithError(e).Error(msg)
		return
	}
	l.Info(msg)
}

func main() {}
