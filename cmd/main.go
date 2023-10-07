package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	serverVersion = "0.1"
	serverName    = "naive-server¯\\_(ツ)_/¯"
)

const (
	statusOK                  = 200
	statusCreated             = 201
	statusInternalServerError = 500
	statusNotFound            = 404
	statusBadRequest          = 400
	statusMethodNotAllowed    = 405
)

const (
	methodGet  = "GET"
	methodPost = "POST"
)

const (
	textStatusOK               = "OK"
	textStatusCreated          = "Created"
	textStatusInternal         = "Internal Server Error"
	textStatusNotFound         = "Not Found"
	textStatusBadRequest       = "Bad Request"
	textStatusMethodNotAllowed = "Method Not Allowed"
)

const (
	contentTypeTextPlain   = "text/plain"
	contentTypeOctetStream = "application/octet-stream"
)

type options struct {
	directory string
	host      string
}

func main() {
	var opts options

	flag.StringVar(&opts.directory, "directory", "./", "the directory to serve files from")
	flag.StringVar(&opts.host, "host", "0.0.0.0:4221", "the host and port to run on")
	flag.Parse()

	l, err := net.Listen("tcp", opts.host)
	if err != nil {
		fmt.Printf("Failed to bind to port %s\n", strings.SplitAfter(opts.host, ":")[1])
		os.Exit(1)
	}

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				fmt.Println("Error accepting connection: ", err.Error())
				os.Exit(1)
			}

			go func() {
				err := handleConn(conn, opts)
				if err != nil {
					fmt.Printf("%v\n", err)
				}
				conn.Close()
			}()
		}
	}()

	select {
	case err := <-errCh:
		fmt.Printf("%v\n", err)
	case sig := <-shutdownCh:
		fmt.Printf("received %d signal\n", sig)
		fmt.Println("server shutdown started")

		// Cleanup

		defer fmt.Println("server shutdown completed")
	}
}

type request struct {
	method        string
	httpVersion   string
	host          string
	userAgent     string
	path          string
	pathParts     []string
	contentLength int
}

type content struct {
	contentType string
	body        []byte
}

func handleConn(conn net.Conn, opts options) error {
	// Parse request
	reqReader := bufio.NewReader(conn)
	reqStr, err := reqReader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("error reading request line bytes: %v\n", err)
	}

	var req request

	n, err := fmt.Sscanf(string(reqStr), "%s %s %s\r\n", &req.method, &req.path, &req.httpVersion)
	if err != nil {
		return fmt.Errorf("error reading request string: %v\n", err)
	}
	if n != 3 {
		return fmt.Errorf("error reading request string: expected 3 parts")
	}

	// Parse path parts
	req.pathParts = strings.Split(strings.Trim(req.path, "\r\n "), "/")

	// Parse headers
	for {
		headerStr, err := reqReader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("error reading header line bytes: %v\n", err)
		}

		// TODO: seems clumbsy :(
		if len(headerStr) == 2 {
			break
		}

		parseHeader(headerStr, &req)
	}

	if len(req.pathParts) < 2 {
		conn.Write(buildResponse(statusBadRequest, nil))

		return nil
	}

	// Handle POST requests
	if req.method == methodPost {
		if req.pathParts[1] == "files" {
			buf := make([]byte, req.contentLength)
			if _, err := reqReader.Read(buf); err != nil {
				conn.Write(buildResponse(statusInternalServerError, nil))
				return fmt.Errorf("error parsing request: %v\n", err)
			}

			err = os.WriteFile(opts.directory+req.pathParts[2], buf, fs.ModeAppend)
			if err != nil {
				conn.Write(buildResponse(statusInternalServerError, nil))
				return fmt.Errorf("error writing %s: %v\n", req.pathParts[2], err)
			}
			conn.Write(buildResponse(statusCreated, nil))

			return nil
		}

		conn.Write(buildResponse(statusNotFound, nil))

		return nil
	}

	// Handle GET requests
	if req.method == methodGet {
		switch req.pathParts[1] {
		case "":
			conn.Write(buildResponse(statusOK, nil))
		case "echo":
			c := content{
				contentType: "text/plain",
				body:        []byte(strings.Join(req.pathParts[2:], "/")),
			}
			conn.Write(buildResponse(statusOK, &c))
		case "user-agent":
			c := content{
				contentType: contentTypeTextPlain,
				body:        []byte(req.userAgent),
			}
			conn.Write(buildResponse(statusOK, &c))
		case "files":
			data, err := os.ReadFile(opts.directory + req.pathParts[2])
			if err != nil {
				conn.Write(buildResponse(statusNotFound, nil))
				return fmt.Errorf("error reading %s: %v\n", req.pathParts[2], err)
			}
			c := content{
				contentType: contentTypeOctetStream,
				body:        data,
			}
			conn.Write(buildResponse(statusOK, &c))
		default:
			conn.Write(buildResponse(statusNotFound, nil))
		}
	}

	conn.Write(buildResponse(statusMethodNotAllowed, nil))

	return nil
}

func parseHeader(line []byte, req *request) {
	parts := strings.Split(string(line), ":")

	switch strings.Trim(parts[0], "\n\r ") {
	case "Host":
		req.host = strings.Trim(parts[1], "\r\n ")
	case "User-Agent":
		req.userAgent = strings.Trim(parts[1], "\r\n ")
	case "Content-Length":
		conLen, err := strconv.Atoi(strings.Trim(parts[1], "\r\n "))
		if err != nil {
			// TODO: do something about it ¯\_(ツ)_/¯
		}
		req.contentLength = conLen
	}
}

func buildResponse(respType int, content *content) []byte {
	var resp bytes.Buffer

	// Add return status
	switch respType {
	case statusOK:
		resp.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusOK, textStatusOK))
	case statusCreated:
		resp.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCreated, textStatusCreated))
	case statusNotFound:
		resp.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusNotFound, textStatusNotFound))
	case statusMethodNotAllowed:
		resp.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusMethodNotAllowed, textStatusMethodNotAllowed))
	}

	// Add headers
	resp.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format("Mon, 02 Jan 2006 15:04:05 MST")))
	resp.WriteString(fmt.Sprintf("Server: %s v%s\r\n", serverName, serverVersion))
	if content != nil {
		resp.WriteString(fmt.Sprintf("Content-Type: %s\r\n", content.contentType))
		resp.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(content.body)))
	}
	resp.WriteString("\r\n")

	if content != nil {
		// Add content
		resp.Write(content.body)
	}

	return resp.Bytes()
}
