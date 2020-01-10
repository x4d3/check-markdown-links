package main

import (
	"flag"
	"fmt"
	"github.com/bmatcuk/doublestar"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const WorkerNumber = 32

func main() {
	rootDir := flag.String("root", "", "a string")
	flag.Parse()
	if len(flag.Args()) != 1 {
		log.Fatal("Argument is missing")
	}

	filePath := flag.Args()[0]
	matches, err := doublestar.Glob(filePath)
	if err != nil {
		log.Fatal(err)
	}
	c := newLinkChecker(*rootDir)

	jobs := make(chan string, len(matches))
	finished := make(chan bool, len(matches))

	for _, filePath := range matches {
		jobs <- filePath
	}
	for w := 0; w < WorkerNumber; w++ {
		go worker(c, jobs, finished)
	}
	close(jobs)

	for i := 0; i < len(matches); i++ {
		<-finished
	}
}

func worker(c *linkChecker, jobs <-chan string, finished chan<- bool) {
	for filePath := range jobs {
		errorsFound := c.checkFile(filePath)
		fmt.Println(filePath)
		if len(errorsFound) > 0 {
			fmt.Println(filePath)
			fmt.Println(strings.Join(errorsFound, "\n"))
			finished <- false
		} else {
			finished <- true
		}
	}
}

func RemoveRight(s string, substr string) string {
	indexOf := strings.LastIndexAny(s, substr)
	if indexOf != -1 {
		return s[0:indexOf]
	} else {
		return s
	}
}

type linkChecker struct {
	items   map[string]string
	mu      sync.RWMutex
	rootDir string
}

func newLinkChecker(rootDir string) *linkChecker {
	items := make(map[string]string)
	return &linkChecker{items: items, rootDir: rootDir}
}

func (c *linkChecker) checkFile(filePath string) []string {
	md, err := ioutil.ReadFile(filePath)
	if err != nil {
		return []string{err.Error()}
	}
	node := markdown.Parse(md, nil)
	links := extractLinks(node)
	var messages []string
	for _, link := range links {
		message := c.checkLink(filePath, link)
		if message != "" {
			messages = append(messages, message)
		}
	}
	return messages
}

func extractLinks(node ast.Node) []string {
	switch node := node.(type) {
	case *ast.Link:
		return []string{string(node.Destination)}
	}
	var links []string
	for _, child := range node.GetChildren() {
		links = append(links, extractLinks(child)...)
	}
	return links
}

func (c *linkChecker) checkLink(filePath string, link string) string {
	c.mu.Lock()
	v, found := c.items[link]
	if !found {
		v = c.doCheckLink(filePath, link)
		c.items[link] = v
	}
	c.mu.Unlock()
	return v
}

func (c *linkChecker) doCheckLink(filePath, link string) string {
	if strings.HasPrefix(link, "mailto") {
		return ""
	}
	if strings.HasPrefix(link, "http") {
		client := http.Client{
			Timeout: time.Duration(1 * time.Second),
		}
		resp, err := client.Get(link)
		if err != nil {
			return err.Error()
		}
		if !(resp.StatusCode >= 200 && resp.StatusCode <= 299) {
			return fmt.Sprint(link, resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		defer resp.Body.Close()
	} else {
		dir := filepath.Dir(filePath)
		if strings.HasPrefix(link, "/") {
			dir = c.rootDir
		}
		linkPath := filepath.Join(dir, RemoveRight(link, "#:"))
		if _, err := os.Stat(linkPath); os.IsNotExist(err) {
			return fmt.Sprint(link, " Could not find file/folder: ", linkPath)
		}
	}
	return ""
}
