// Package main implements a sitemap builder that crawls websites and generates XML sitemaps.
// This tool follows the sitemap protocol defined at https://www.sitemaps.org/
package main

import (
	"flag"
	"fmt"
	"time"

	"net/http"
	"sitemap_builder/parse"
)

// main is the entry point of the sitemap builder application.
// It parses command-line flags, crawls the specified website using BFS algorithm,
// and outputs a valid XML sitemap to stdout.
func main() {
	// Create an HTTP client with a reasonable timeout to prevent hanging requests
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Parse command-line arguments for URL and crawling depth
	urlPtr := flag.String("url", "https://gophercises.com", "URL to fetch and parse")
	maxDepth := flag.Int("depth", 3, "Maximum number of links deep to traverse")
	flag.Parse()

	// Display crawling configuration
	fmt.Println("Max Depth:", *maxDepth)
	fmt.Println("Fetching URL:", *urlPtr)
	fmt.Println("--------------------------------------------------------------------------")

	// Fetch and parse the initial HTML document
	doc, err := parse.FetchAndParse(*urlPtr, client)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Use the provided URL as the base domain for internal link detection
	baseDomain := *urlPtr

	// Extract all internal links from the initial page
	initialLinks := parse.ExtractLinks(doc, baseDomain)

	// Perform breadth-first search crawling to discover all internal pages
	allLinks, err := parse.CrawlBFS(initialLinks, *maxDepth, client)
	if err != nil {
		fmt.Println("Error during crawling:", err)
		return
	}

	// Generate XML sitemap from discovered links
	sitemapXML, err := parse.EncodeXML(allLinks)
	if err != nil {
		fmt.Println("Error encoding XML:", err)
		return
	}

	// Output the final sitemap to stdout
	fmt.Println(sitemapXML)
}
