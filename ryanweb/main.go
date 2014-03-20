package main

import (
	//"bufio"
	"flag"
	"fmt"
	//"html"
	"html/template"
	"io"
	//"image"
	//"image/jpeg"
	"log"
	//"mime"
	"net"
	"net/http"
	//"net/url"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
)

import (
//"github.com/JamesDunne/go-ryan/resize"
)

var proxyRoot, picsDir string
var templates *template.Template

func pjoin(a, b string) string {
	if strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/") {
		return a + b[1:]
	} else if strings.HasSuffix(a, "/") {
		return a + b
	} else if strings.HasPrefix(b, "/") {
		return a + b
	} else {
		return a + "/" + b
	}
}

func removeIfStartsWith(s, start string) string {
	if !strings.HasPrefix(s, start) {
		return s
	}
	return s[len(start):]
}

// For directory entry sorting:

type Entries []os.FileInfo

func (s Entries) Len() int      { return len(s) }
func (s Entries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type sortBy int

const (
	sortByName sortBy = iota
	sortByDate
	sortBySize
)

type sortDirection int

const (
	sortAscending sortDirection = iota
	sortDescending
)

// Sort by name:
type ByName struct {
	Entries
	dir sortDirection
}

func (s ByName) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Name() < s.Entries[j].Name()
	} else {
		return s.Entries[i].Name() > s.Entries[j].Name()
	}
}

// Sort by last modified time:
type ByDate struct {
	Entries
	dir sortDirection
}

func (s ByDate) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].ModTime().Before(s.Entries[j].ModTime())
	} else {
		return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
	}
}

// Sort by size:
type BySize struct {
	Entries
	dir sortDirection
}

func (s BySize) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Size() < s.Entries[j].Size()
	} else {
		return s.Entries[i].Size() > s.Entries[j].Size()
	}
}

// Logging+action functions
func doError(req *http.Request, rsp http.ResponseWriter, msg string, code int) {
	http.Error(rsp, msg, code)
}

func doRedirect(req *http.Request, rsp http.ResponseWriter, url string, code int) {
	http.Redirect(rsp, req, url, code)
}

func getPics() []os.FileInfo {
	// Open the directory to read its contents:
	f, err := os.Open(picsDir)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Read the directory entries:
	fis, err := f.Readdir(0)
	if err != nil {
		panic(err)
	}

	// Remove the opened files from the list (presume they are in mid-upload via SFTP):

	// Sort the entries by the desired mode:
	sort.Sort(ByDate{fis, sortDescending})

	return fis
}

// HTML handler for `/`:
func indexHandler(rsp http.ResponseWriter, req *http.Request) {
	if req.URL.Path != proxyRoot+"/" {
		http.Error(rsp, "404 Not Found", http.StatusNotFound)
		return
	}

	var rsperr error

	defer func() {
		if err := recover(); err != nil {
			var ok bool
			if rsperr, ok = err.(error); !ok {
				// Format the panic as a string if it's not an `error`:
				rsperr = fmt.Errorf("%v", err)
			}
		}

		if rsperr != nil {
			doError(req, rsp, rsperr.Error(), http.StatusInternalServerError)
			return
		}
	}()

	// Read the directory:
	fis := getPics()

	defer func() {
		if rsperr != nil {
			return
		}

		// Successful response:
		rsp.Header().Add("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(http.StatusOK)
		templates.ExecuteTemplate(rsp, "index.html", fis)
	}()
}

// HTML handler for `/upload`:
func uploadHandler(rsp http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(rsp, "Method requires POST", http.StatusMethodNotAllowed)
		return
	}

	pnk := try(func() {
		reader, err := req.MultipartReader()
		if err != nil {
			panic(err)
		}

		// Keep reading the multipart form data and handle file uploads:
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FileName() == "" {
				continue
			}

			// Copy upload data to a local file:
			destPath := path.Join(picsDir, part.FileName())
			log.Printf("Upload: '%s' to '%s'\n", part.FileName(), destPath)

			f, err := os.Create(destPath)
			if err != nil {
				panic(fmt.Errorf("Could not create local file '%s'; error: %s", destPath, err.Error()))
			}
			defer f.Close()

			if _, err := io.Copy(f, part); err != nil {
				panic(err)
			}
		}
	})

	// Handle the panic:
	if pnk != nil {
		if err, ok := pnk.(error); ok {
			http.Error(rsp, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(rsp, fmt.Sprint("%v", pnk), http.StatusInternalServerError)
		}
		return
	}

	// 302 to `/`:
	http.Redirect(rsp, req, proxyRoot+"/", http.StatusFound)
}

func extractNames(fis []os.FileInfo) []string {
	names := make([]string, len(fis), len(fis))
	for i := range fis {
		names[i] = fis[i].Name()
	}
	return names
}

// JSON handler for `/list.php`:
func listJsonHandler(req *http.Request) (result interface{}) {
	fis := getPics()

	return struct {
		BaseUrl string   `json:"baseUrl"`
		Files   []string `json:"files"`
	}{
		BaseUrl: "http://bittwiddlers.org/ryan/pics/",
		Files:   extractNames(fis),
	}
}

// JSON handler for `/delete`:
func deleteJsonHandler(req *http.Request) (result interface{}) {
	if req.Method != "POST" {
		panic(fmt.Errorf("Method requires POST"))
	}

	return struct {
		Success bool `json:"success"`
	}{
		Success: true,
	}
}

// File server for `/thumbs/*`:
func thumbHandler(rsp http.ResponseWriter, req *http.Request) {

	//req.URL
}

func main() {
	var socketType string
	var socketAddr string
	var templatesDir string

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	flag.StringVar(&socketType, "l", "tcp", `type of socket to listen on; "unix" or "tcp" (default)`)
	flag.StringVar(&socketAddr, "a", ":8080", `address to listen on; ":8080" (default TCP port) or "/path/to/unix/socket"`)

	flag.StringVar(&proxyRoot, "p", "/ryan", "root of web requests to process")
	flag.StringVar(&templatesDir, "tmpl", "./tmpl", "local filesystem path to HTML templates")
	flag.StringVar(&picsDir, "pics", "./pics", "local filesystem path to store pictures")
	flag.Parse()

	if strings.HasSuffix(proxyRoot, "/") {
		proxyRoot = proxyRoot[0 : len(proxyRoot)-1]
	}

	// Parse HTML templates:
	templates = template.Must(template.ParseGlob(path.Join(templatesDir, "*.html")))

	// Create the socket to listen on:
	l, err := net.Listen(socketType, socketAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	// NOTE(jsd): Unix sockets must be unlink()ed before being reused again.

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a signal:
		sig := <-c
		log.Printf("Caught signal '%s': shutting down.", sig)
		// Stop listening:
		l.Close()
		// Delete the unix socket, if applicable:
		if socketType == "unix" {
			os.Remove(socketAddr)
		}
		// And we're done:
		os.Exit(0)
	}(sigc)

	// Set up the request multiplexer:
	mux := http.NewServeMux()
	mux.HandleFunc(pjoin(proxyRoot, "/"), indexHandler)
	mux.HandleFunc(pjoin(proxyRoot, "/upload"), uploadHandler)

	mux.Handle(pjoin(proxyRoot, "/list"), NewJsonHandler(listJsonHandler))
	mux.Handle(pjoin(proxyRoot, "/list.php"), NewJsonHandler(listJsonHandler))
	mux.Handle(pjoin(proxyRoot, "/delete"), NewJsonHandler(deleteJsonHandler))

	// Serve /pics/ from the folder:
	mux.Handle(pjoin(proxyRoot, "/pics/"), http.StripPrefix(pjoin(proxyRoot, "/pics/"), http.FileServer(http.Dir(picsDir))))
	// Serve /thumbs/ requests dynamically with a filesystem-backed cache:
	mux.HandleFunc(pjoin(proxyRoot, "/thumbs/"), thumbHandler)

	// Start the HTTP server on the listening socket:
	log.Fatal(http.Serve(l, http.Handler(mux)))
}
