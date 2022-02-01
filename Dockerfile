FROM golang:1.17-alpine

RUN apk add --no-cache strace
COPY . /app
WORKDIR /app
RUN go build .