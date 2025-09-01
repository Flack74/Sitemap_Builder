// Package parse provides functionality for web crawling, HTML parsing, and XML sitemap generation.
// It implements a breadth-first search algorithm to discover internal links and follows
// the XML sitemap protocol specification.
package parse

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Link represents an HTML anchor element with its URL and text content.
// This structure is used internally during the crawling process.
type Link struct {
	Href string // The URL/href attribute of the link
	Text string // The visible text content of the link
}

// Urlset represents the root element of an XML sitemap according to the sitemap protocol.
// It contains the XML namespace and a collection of URL entries.
type Urlset struct {
	XMLName xml.Name `xml:"urlset"`                                    // Root XML element name
	Xmlns   string   `xml:"xmlns,attr"`                               // XML namespace attribute
	Urls    []Url    `xml:"url"`                                      // Collection of URL entries
}

// Url represents a single URL entry in the XML sitemap.
// Each entry contains the location (URL) of a page on the website.
type Url struct {
	Loc string `xml:"loc"` // The URL location of the page
}

// FetchAndParse retrieves an HTML document from the specified URL and parses it into a DOM tree.
// It handles HTTP requests with proper headers and error handling, returning a parsed HTML node tree
// that can be traversed to extract links and other content.
//
// Parameters:
//   - url: The URL to fetch and parse
//   - client: HTTP client with configured timeout and other settings
//
// Returns:
//   - *html.Node: Root node of the parsed HTML document
//   - error: Any error that occurred during fetching or parsing
func FetchAndParse(url string, client *http.Client) (*html.Node, error) {
	// Create a new HTTP GET request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil, fmt.Errorf("creating request for URL %s: %w", url, err)
	}

	// Set User-Agent header to avoid being blocked by websites that reject bot requests
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SitemapBuilder/1.0)")

	// Execute the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching URL %s: %w", url, err)
	}
	defer resp.Body.Close() // Ensure response body is closed to prevent resource leaks

	// Check for successful HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching URL %s: received status code %d", url, resp.StatusCode)
	}

	// Parse the HTML response body into a DOM tree
	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Println("Error parsing HTML:", err)
		return nil, fmt.Errorf("parsing HTML from %s: %w", url, err)
	}

	return doc, nil
}

// extractText recursively extracts and concatenates all text content from an HTML node and its children.
// It traverses the DOM tree depth-first, collecting text from all text nodes and normalizing whitespace.
// This function is used to get the visible text content of anchor elements for link descriptions.
//
// Parameters:
//   - n: The HTML node to extract text from
//
// Returns:
//   - string: Normalized text content with excess whitespace removed
func extractText(n *html.Node) string {
	// Base case: if this is a text node, return its content
	if n.Type == html.TextNode {
		return n.Data
	}

	// Skip non-element nodes (comments, etc.)
	if n.Type != html.ElementNode {
		return ""
	}

	// Recursively collect text from all child nodes
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(extractText(c))
	}

	// Normalize whitespace: split on any whitespace and rejoin with single spaces
	return strings.Join(strings.Fields(sb.String()), " ")
}

// ExtractLinks traverses an HTML document tree and extracts all internal links (anchor elements).
// It performs a depth-first traversal of the DOM, identifying anchor tags with href attributes
// that point to internal pages within the same domain. Duplicate links are automatically filtered out.
//
// Parameters:
//   - n: Root HTML node to start traversal from
//   - baseDomain: Base domain URL used to determine if links are internal
//
// Returns:
//   - []Link: Slice of unique internal links found in the document
func ExtractLinks(n *html.Node, baseDomain string) []Link {
	var links []Link
	// Use a map to track seen URLs and prevent duplicates
	seen := make(map[string]struct{})

	// Define a recursive function to walk the DOM tree
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		// Check if current node is an anchor element
		if node.Type == html.ElementNode && node.DataAtom == atom.A {
			// Look for href attribute in the anchor element
			for _, attr := range node.Attr {
				if attr.Key == "href" && isInternalLink(attr.Val, baseDomain) {
					href := attr.Val

					// Convert relative URLs to absolute URLs
					if strings.HasPrefix(href, "/") {
						href = resolveURL(baseDomain, href)
					}

					// Add link only if we haven't seen it before
					if _, exists := seen[href]; !exists {
						seen[href] = struct{}{}
						links = append(links, Link{
							Href: href,
							Text: strings.TrimSpace(extractText(node)),
						})
					}
					break // Found href attribute, no need to check other attributes
				}
			}
		}

		// Recursively process all child nodes
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	// Start the recursive traversal from the root node
	walk(n)
	return links
}

// isInternalLink determines whether a given link URL is internal to the website being crawled.
// A link is considered internal if it's either a relative path (starts with "/") or
// an absolute URL that begins with the base domain.
//
// Parameters:
//   - link: The URL to check
//   - baseDomain: The base domain of the website being crawled
//
// Returns:
//   - bool: true if the link is internal, false otherwise
func isInternalLink(link, baseDomain string) bool {
	// Relative paths (e.g., "/about", "/contact") are always internal
	// Absolute URLs starting with the base domain are also internal
	return strings.HasPrefix(link, "/") || strings.HasPrefix(link, baseDomain)
}

// CrawlBFS performs a breadth-first search crawl of a website starting from the provided links.
// It systematically visits pages level by level, extracting internal links from each page
// and adding them to the crawl queue. The crawling stops when the maximum depth is reached
// or when all discoverable internal pages have been visited.
//
// The BFS approach ensures that pages closer to the starting point are crawled first,
// which is ideal for sitemap generation as it prioritizes more important/accessible pages.
//
// Parameters:
//   - links: Initial set of links to start crawling from
//   - maxDepth: Maximum depth to crawl (0 = only initial links, 1 = one level deep, etc.)
//   - client: HTTP client for making requests
//
// Returns:
//   - []Link: All unique internal links discovered during the crawl
//   - error: Any error that prevented the crawl from starting
func CrawlBFS(links []Link, maxDepth int, client *http.Client) ([]Link, error) {
	// Validate input
	if len(links) == 0 {
		return nil, fmt.Errorf("no links to traverse")
	}

	// Track visited URLs to avoid infinite loops and duplicate processing
	visited := make(map[string]struct{})

	// Node represents a link with its depth in the crawl tree
	type Node struct {
		link  Link // The link being processed
		depth int  // How many levels deep this link is from the starting point
	}

	// Initialize BFS queue with the first link at depth 0
	visited[links[0].Href] = struct{}{}
	queue := []Node{{links[0], 0}}

	// Store all discovered links for the final sitemap
	var result []Link

	// Process queue until empty (BFS main loop)
	for len(queue) > 0 {
		// Dequeue the next node to process
		currentNode := queue[0]
		queue = queue[1:]

		// Add current link to results
		result = append(result, currentNode.link)

		// Skip further crawling if we've reached maximum depth
		if currentNode.depth >= maxDepth {
			continue
		}

		// Fetch and parse the current page to find more internal links
		doc, err := FetchAndParse(currentNode.link.Href, client)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch %s: %v\n", currentNode.link.Href, err)
			continue // Skip this page but continue crawling others
		}

		// Extract all internal links from the current page
		neighbors := ExtractLinks(doc, currentNode.link.Href)

		// Add unvisited neighbors to the queue for future processing
		for _, neighbor := range neighbors {
			if _, alreadyVisited := visited[neighbor.Href]; !alreadyVisited {
				visited[neighbor.Href] = struct{}{}
				queue = append(queue, Node{neighbor, currentNode.depth + 1})
			}
		}
	}

	return result, nil
}

// resolveURL converts a relative URL to an absolute URL using the provided base URL.
// This function handles the conversion of relative paths (e.g., "/about", "../contact")
// to fully qualified URLs that can be used for HTTP requests.
//
// Parameters:
//   - base: The base URL to resolve relative URLs against
//   - href: The URL to resolve (can be relative or absolute)
//
// Returns:
//   - string: The resolved absolute URL, or the original href if resolution fails
func resolveURL(base, href string) string {
	// Parse the href to determine if it's already absolute
	hrefURL, err := url.Parse(href)
	if err != nil {
		// If parsing fails, return the original href as fallback
		return href
	}

	// If the href is already an absolute URL, return it as-is
	if hrefURL.IsAbs() {
		return hrefURL.String()
	}

	// Parse the base URL for resolution
	baseURL, err := url.Parse(base)
	if err != nil {
		// If base URL parsing fails, return the original href
		return href
	}

	// Resolve the relative URL against the base URL
	return baseURL.ResolveReference(hrefURL).String()
}

// EncodeXML converts a slice of Link structs into a properly formatted XML sitemap.
// The generated XML follows the sitemap protocol specification (https://www.sitemaps.org/protocol.html)
// and includes the required XML header and namespace declarations.
//
// The output is formatted with proper indentation for human readability and can be
// directly saved as a sitemap.xml file or served to search engines.
//
// Parameters:
//   - links: Slice of Link structs containing the URLs to include in the sitemap
//
// Returns:
//   - string: Complete XML sitemap as a string with proper formatting
//   - error: Any error that occurred during XML marshaling
func EncodeXML(links []Link) (string, error) {
	// Convert Link structs to Url structs for XML serialization
	// We only need the URL location for the sitemap, not the link text
	urls := make([]Url, 0, len(links))
	for _, link := range links {
		urls = append(urls, Url{Loc: link.Href})
	}

	// Create the root urlset element with proper namespace
	urlset := Urlset{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", // Required sitemap namespace
		Urls:  urls,
	}

	// Marshal the structure to XML with proper indentation for readability
	output, err := xml.MarshalIndent(urlset, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling XML: %w", err)
	}

	// Prepend the standard XML declaration header
	return xml.Header + string(output), nil
}
