package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/kballard/go-shellquote"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalhostTCPConnection creates a TCP connection over localhost and send the
// data from r to it. The data read on the server side will be written to w.
//
// This function uses the deadline from ctx to prevent indefinite blocking, if
// provided.
func LocalhostTCPConnect(ctx context.Context, log *logrus.Entry, r io.Reader, w io.Writer) (sent int64, e error) {
	dl, dlOk := ctx.Deadline()

	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0, // Randomly chosen open port.
	})
	if err != nil {
		return 0, fmt.Errorf("unable to create TCP listener: %w", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			log.WithError(err).Error("close TCP listener")
		}
	}()
	if dlOk {
		if err := l.SetDeadline(dl); err != nil {
			return 0, fmt.Errorf("unable to set listener deadline: %w", err)
		}
	}

	var g, _ = errgroup.WithContext(ctx)
	g.Go(func() error {
		conn, err := l.Accept()
		if err != nil {
			return fmt.Errorf("error accepting connection: %w", err)
		}
		if dlOk {
			if err := conn.SetReadDeadline(dl); err != nil {
				return fmt.Errorf("unable to set server connection deadline: %w", err)
			}
		}
		_, err = io.Copy(w, conn)
		if err != nil {
			return fmt.Errorf("error copying data from server connection: %w", err)
		}
		return nil
	})

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", l.Addr().String())
	if err != nil {
		return 0, fmt.Errorf("unable to connect to loopback TCP listener: %w", err)
	}

	sent, err = io.Copy(conn, r)
	log = log.WithFields(logrus.Fields{
		"destination address": conn.RemoteAddr().String(),
		"source address":      conn.LocalAddr().String(),
		"protocol":            conn.LocalAddr().Network(),
		"data sent (bytes)":   sent,
	})
	errLog(log, err, "data transmission")
	if err != nil {
		return sent, fmt.Errorf("error copying data to client connection: %w", err)
	}
	if err := conn.Close(); err != nil {
		return sent, fmt.Errorf("error closing client connection: %w", err)
	}

	if err := g.Wait(); err != nil {
		return sent, fmt.Errorf("server listener error: %w", err)
	}
	return sent, nil
}

func errLog(l *logrus.Entry, e error, msg string) {
	if e != nil {
		l.WithError(e).Error(msg)
		return
	}
	l.Info(msg)
}

// RunActivities runs five different activities to trigger events with the EDR
// agent.
func RunActivities(ctx context.Context, l *logrus.Logger, directory, fileExt string, arguments []string) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("unable to get user information: %w", err)
	}
	log := l.WithFields(logrus.Fields{
		"username":             u.Username,
		"process name":         os.Args[0],
		"process command line": strings.Join(os.Args, " "),
		"process ID":           os.Getpid(),
	})

	// Run process.
	cmd := exec.CommandContext(ctx, "echo", arguments...)
	err = cmd.Start()
	errLog(log, err, "process start")
	if err != nil {
		return fmt.Errorf("error starting the command: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command error: %w", err)
	}

	// Create file.
	if directory == "" {
		directory = os.TempDir()
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("unable to get absolute path for directory: %w", err)
	}
	r := rand.New(rand.NewSource(time.Now().Unix()))
	path := filepath.Join(directory, strconv.Itoa(r.Intn(1_000_000))+fileExt)
	fLog := log.WithFields(logrus.Fields{
		"file path":     path,
		"file activity": "create",
	})
	f, err := os.Create(path)
	errLog(fLog, err, "create file")
	if err != nil {
		return fmt.Errorf("unable to create file: %w", err)
	}

	// Run code in closure with defer to ensure attempt is made to close and
	// delete file before moving on to the network transmission activity.
	err = func() (e error) {
		defer func() {
			// Close file.
			if err := f.Close(); err != nil {
				log.WithError(err).Error("close file")
			}

			// Delete file.
			err = os.Remove(f.Name())
			errLog(fLog.WithField("file activity", "delete"), err, "delete file")
			if err != nil && e == nil {
				e = fmt.Errorf("unable to delete file: %w", err)
			}
		}()

		// Modify file.
		_, err = f.WriteString("file append")
		errLog(fLog.WithField("file activity", "modify"), err, "modify file")
		if err != nil {
			return fmt.Errorf("unable to write to string: %w", err)
		}
		return nil
	}()
	if err != nil {
		return err
	}

	// Network connection and data transmission.
	_, err = LocalhostTCPConnect(ctx, log, strings.NewReader("hello"), io.Discard)
	if err != nil {
		return fmt.Errorf("unable to transmit data over a network connection: %w", err)
	}

	return nil
}

func main() {
	// Get the command line arguments.
	dir := flag.String("file-directory", "", "directory to create file (defaults to OS temporary directory)")
	ext := flag.String("file-extension", ".txt", "extension for file")
	args := flag.String("process-arguments", "", "arguments for program to run (shell quoted)")
	flag.Parse()

	// Use structured JSON for the output format.
	l := logrus.New()
	l.Formatter = &logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "timestamp",
		},
	}

	// Parse out the process arguments like a POSIX shell.
	pArgs, err := shellquote.Split(*args)
	if err != nil {
		l.Fatalf("Invalid process arguments: %v", err)
	}

	// Run the activities with a timeout to hopefully stop the program in the
	// event of unexpectedly long blocking call.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RunActivities(ctx, l, *dir, *ext, pArgs); err != nil {
		l.Fatalf("Error running activities: %v", err)
	}
}
