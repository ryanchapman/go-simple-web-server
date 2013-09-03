/*

From https://github.com/cespare/go-apachelog

Copyright (c) 2012 Caleb Spare

MIT License

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

Package apachelog is a library for logging the responses of an http.Handler in the Apache Common Log Format.
It uses a variant of this log format (http://httpd.apache.org/docs/1.3/logs.html#common) also used by
Rack::CommonLogger in Ruby. The format has an additional field at the end for the response time in seconds.

Using apachelog is typically very simple. You'll need to create an http.Handler and set up your request
routing first. In a simple web application, for example, you might just use http.NewServeMux(). Next, wrap the
http.Handler in an apachelog handler using NewHandler, create an http.Server with this handler, and you're
good to go.

Example:

		mux := http.NewServeMux()
		mux.HandleFunc("/", handler)
		loggingHandler := apachelog.NewHandler(mux, os.Stderr)
		server := &http.Server{
			Addr: ":8899",
			Handler: loggingHandler,
		}
		server.ListenAndServe()
*/
package apachelog

import (
	"fmt"
	"io"
	"net"
	"net/http"
    "strings"
	"time"
)

// Using a variant of apache common log format used in Ruby's Rack::CommonLogger which includes response time
// in seconds at the the end of the log line.
const apacheFormatPattern = "%s:%s - - [%s] \"%s %s %s\" %d %d %0.4f\n"

// record is a wrapper around a ResponseWriter that carries other metadata needed to write a log line.
type record struct {
	http.ResponseWriter

	ip                    string
    port                  string
	time                  time.Time
	method, uri, protocol string
	status                int
	responseBytes         int64
	elapsedTime           time.Duration
}

// Log writes the record out as a single log line to out.
func (r *record) Log(out io.Writer) {
	timeFormatted := r.time.Format("02/Jan/2006 15:04:05")
	fmt.Fprintf(out, apacheFormatPattern, r.ip, r.port, timeFormatted, r.method, r.uri, r.protocol, r.status,
		r.responseBytes, r.elapsedTime.Seconds())
}

// Write proxies to the underlying ResponseWriter.Write method while recording response size.
func (r *record) Write(p []byte) (int, error) {
	written, err := r.ResponseWriter.Write(p)
	r.responseBytes += int64(written)
	return written, err
}

// WriteHeader proxies to the underlying ResponseWriter.WriteHeader method while recording response status.
func (r *record) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// handler is an http.Handler that logs each response.
type handler struct {
	http.Handler
	out io.Writer
}

// NewHandler creates a new http.Handler, given some underlying http.Handler to wrap and an output stream
// (typically os.Stderr).
func NewHandler(h http.Handler, out io.Writer) http.Handler {
	return &handler{
		Handler: h,
		out:     out,
	}
}

// ServeHTTP delegates to the underlying handler's ServeHTTP method and writes one log line for every call.
func (h *handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	record := &record{
		ResponseWriter: rw,
		ip:             getIP(r.RemoteAddr),
        port:           getPort(r),
		time:           time.Time{},
		method:         r.Method,
		uri:            r.RequestURI,
		protocol:       r.Proto,
		status:         http.StatusOK,
		elapsedTime:    time.Duration(0),
	}

	startTime := time.Now()
	h.Handler.ServeHTTP(record, r)
	finishTime := time.Now()

	record.time = finishTime
	record.elapsedTime = finishTime.Sub(startTime)

	record.Log(h.out)
}

// A best-effort attempt at getting the IP from http.Request.RemoteAddr. For a Go server, they typically look
// like this:
// 127.0.0.1:36341
// [::1]:44092
// I think this is standard for IPv4 and IPv6 addresses.
func getIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
    // make sure IPv6 addresses are surrounded by brackets
    if strings.Contains(host, ":") {
        if host[0] != '[' {
            host = fmt.Sprintf("[%s]", host)
        }
    }
	return host
}

func getPort(r *http.Request) string {
    _, port, err := net.SplitHostPort(r.Host)
    if err != nil {
        // default ports (80/443) do not show up in r.Host
        if r.TLS == nil {
            return "80"
        } else {
            return "443"
        }
    }
    return port
}
