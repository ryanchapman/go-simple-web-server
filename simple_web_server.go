//
// simple_web_server.go - Simple web server that serves files from current directory
//
// Copyright (C) 2013 Ryan A. Chapman. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
//   1. Redistributions of source code must retain the above copyright notice,
//      this list of conditions and the following disclaimer.
//
//   2. Redistributions in binary form must reproduce the above copyright notice,
//      this list of conditions and the following disclaimer in the documentation
//      and/or other materials provided with the distribution.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES,
// INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND
// FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHORS
// OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
// EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS;
// OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY,
// WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR
// OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
// ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// Ryan A. Chapman, ryan@rchapman.org
// Sun Sep  1 20:41:08 MDT 2013

package main

import (
    apachelog "./go-apachelog"
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "flag"
    "fmt"
    "log"
    "math/big"
    "net"
    "net/http"
    "os"
    "os/signal"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"
)

const VERSION = "1.0"

var randuint uint32
var randmu sync.Mutex
var gHTTPPortsCSV  string
var gHTTPSPortsCSV string
var gHTTPPorts     []string
var gHTTPSPorts    []string
var gCertFile      string = tempFilename("cert.pem")
var gKeyFile       string = tempFilename("key.pem")

func tempFilename(prefix string) (fileName string) {
    dir := os.Getenv("TMPDIR")
    if dir == "" {
        dir = os.Getenv("TMP")
        if dir == "" {
            dir = os.Getenv("TEMP")  // windows
            if dir == "" {
                dir = "/tmp"
            }
        }
    }
    // http://golang.org/src/pkg/io/ioutil/tempfile.go
    nconflict := 0
    for i := 0; i < 10000; i++ {
        fileName = filepath.Join(dir, prefix+nextSuffix())
        f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
        if os.IsExist(err) {
            if nconflict++; nconflict > 10 {
                randuint = reseed()
            }
            continue
        }
        f.Close()
        break
    }
    return 
}

// http://golang.org/src/pkg/io/ioutil/tempfile.go
func nextSuffix() string {
    randmu.Lock()
    r := randuint
    if r == 0 {
        r = reseed()
    }
    r = r*1664525 + 1013904223 // constants from Numerical Recipes
    randuint = r
    randmu.Unlock()
    return strconv.Itoa(int(1e9 + r%1e9))[1:]
}

// http://golang.org/src/pkg/io/ioutil/tempfile.go
func reseed() uint32 {
    return uint32(time.Now().UnixNano() + int64(os.Getpid()))
}

func generateSelfSignedCert() () {
    // from http://golang.org/src/pkg/crypto/tls/generate_cert.go
    priv, err := rsa.GenerateKey(rand.Reader, 1024)
    if err != nil {
        log.Fatalf("failed to generate private key: %s", err)
        return
    }
    template := x509.Certificate{
        SerialNumber: new(big.Int).SetInt64(0),
        Subject: pkix.Name{
            Organization: []string{"Acme Co"},
        },
        NotBefore:             time.Now(),
        NotAfter:              time.Now().Add(365*24*time.Hour),
        KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
        ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        BasicConstraintsValid: true,
        IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
        DNSNames:              []string{"localhost"},
    }
    derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
    if err != nil {
        log.Fatalf("Failed to create certificate: %s", err)
        return
    }
    certOut, err := os.Create(gCertFile)
    if err != nil {
        log.Fatalf("failed to open %s for writing: %s", gCertFile, err)
        return
    }
    pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
    certOut.Close()

    keyOut, err := os.OpenFile(gKeyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
    if err != nil {
        log.Print("failed to open %s for writing:", gKeyFile, err)
        return
    }
    pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
    keyOut.Close()
    return
}


func versionString() (v string) {
    buildNum := strings.ToUpper(strconv.FormatInt(BUILDTIMESTAMP, 36))
    buildDate := time.Unix(BUILDTIMESTAMP, 0).Format(time.UnixDate)
    v = fmt.Sprintf("simple_web_server %s (build %v, %v by %v@%v)", VERSION, buildNum, buildDate, BUILDUSER, BUILDHOST)
    return
}

func init() {
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "%s\n\n", versionString())
        fmt.Fprintf(os.Stderr, "usage: %s -p http_ports -sp https_ports\n", os.Args[0])
        fmt.Fprintf(os.Stderr, "Optional\n")
        fmt.Fprintf(os.Stderr, "  -p=PORTS     HTTP ports to listen on, separared by commas. Defaults to 80\n")
        fmt.Fprintf(os.Stderr, "  -sp=PORTS    HTTPS (SSL) ports to listen on, separared by commas. Defaults to 443\n")
        fmt.Fprintf(os.Stderr, "Report bugs to <ryan@rchapman.org>.\n")
    }
    flag.StringVar(&gHTTPPortsCSV,  "p",  "80",  "HTTP ports to listen on, separated by commas. E.g. -p 80,8080")
    flag.StringVar(&gHTTPSPortsCSV, "sp", "443", "HTTPS ports to listen on, separated by commas. E.g. -p 443,4433")
}

func cleanup() {
    os.Remove(gCertFile)
    os.Remove(gKeyFile)
}

func main() {
    // Handle Ctrl-C
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt)
    go func() {
        for _ = range c {
            fmt.Printf("\nCtrl-C: ")
            cleanup()
            os.Exit(1)
        }
    }()

    flag.Parse()

    if gHTTPPortsCSV == "80" {
        gHTTPPorts = []string{"80"}
    } else {
        gHTTPPorts = strings.Split(gHTTPPortsCSV, ",")
    }

    if gHTTPSPortsCSV == "443" {
        gHTTPSPorts = []string{"443"}
    } else {
        gHTTPSPorts = strings.Split(gHTTPSPortsCSV, ",")
    }

    mux := http.NewServeMux()
    mux.Handle("/", http.FileServer(http.Dir(".")))
    loggingHandler := apachelog.NewHandler(mux, os.Stdout)
    wg := sync.WaitGroup{}

    for _, port := range gHTTPPorts {
        server := &http.Server{
            Addr:    fmt.Sprintf(":%s", port),
            Handler: loggingHandler,
        }
        wg.Add(1)
        go func() {
            defer wg.Done()
            server.ListenAndServe()
        }()
        fmt.Printf("Listening on port %s\n", port)
    }
    
    generateSelfSignedCert()
    for _, port := range gHTTPSPorts {
        server := &http.Server{
            Addr:    fmt.Sprintf(":%s", port),
            Handler: loggingHandler,
        }
        wg.Add(1)
        go func() {
            defer wg.Done()
            server.ListenAndServeTLS(gCertFile, gKeyFile)
        }()
        fmt.Printf("Listening on port %s\n", port)
    }

    wg.Wait()
    cleanup()
}


