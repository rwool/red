# red

This program is responsible for executing a series of "activities" that will be observable from other processes if properly configured. The following actions are performed by this program:

* Run the `echo` program with configurable arguments
* Creation, modification, and deletion of a file
* Send data via a TCP connection to a localhost address

This program will log each of these events with enough information to correlate them with events captured elsewhere on the system.

The arguments to run the program are as follows:
* `-file-directory`: This is the directory in which to create, modify, and delete the file mentioned above. The default to to create the file in the OS temporary directory.
* `-file-extension`: This is the file extension to add to the randomly generated file name.
* `-process-arguments`: These are the arguments to run the `echo` program with.

This program is intended to be a relatively simple series of calls to run the relevant activities in series. This is all coordinated from the `RunActivities` function.
Structured logging is maintained throughout the "activities" to share the commonly logged fields as much as possible. The log level changes depending on whether the relevant activity encounters an error.

The network connection and data transmission is more complicated than the rest of the program as a local TCP listener is made to ensure there is always somewhere to make a client TCP connection to.

This code has been tested with macOS and Linux.

## Running
To run the program, Go 1.17 or higher must be installed.
To run (from the root of the project):
```shell
go run ./red.go
```

Arguments can be passed as follows:
```shell
go run ./red.go -file-directory='/tmp' -file-extension='.bin' -process-arguments='hello world'
```

## Verification:
The events can be confirmed to be visible via either dtruss on macOS or strace in Linux:

For macOS:
```shell
go build .
sudo dtruss -f sudo -u "${USER}" ./red
```

For Linux (with sufficiently privileged user):
```shell
go build .
strace ./red
```

For Linux via Dockerfile:
```shell
docker build -t red .
docker run -it --rm --privileged red strace /app/red
```