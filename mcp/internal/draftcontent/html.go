package draftcontent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

var emailHTMLPolicy = func() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "b", "em", "i", "u", "s", "strike", "ul", "ol", "li", "blockquote")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("start").Matching(regexp.MustCompile(`^[1-9][0-9]{0,8}$`)).OnElements("ol")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	return policy
}()

func Prepare(subject, body, bodyHTML string) (string, string, string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 998 || strings.ContainsAny(subject, "\r\n") || !utf8.ValidString(subject) {
		return "", "", "", errors.New("provide a subject of at most 998 bytes without line breaks")
	}
	if len(body) > 100000 || len(bodyHTML) > 200000 || !utf8.ValidString(body) || !utf8.ValidString(bodyHTML) {
		return "", "", "", errors.New("email text must be valid UTF-8 and fit within 100,000 bytes of text and 200,000 bytes of formatting")
	}
	bodyHTML = strings.TrimSpace(bodyHTML)
	if bodyHTML != "" {
		bodyHTML = emailHTMLPolicy.Sanitize(bodyHTML)
		document, err := html.Parse(strings.NewReader(bodyHTML))
		if err != nil {
			return "", "", "", errors.New("could not read the email formatting")
		}
		body, err = htmlText(document, 0)
		if err != nil {
			return "", "", "", err
		}
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 100000 || len(bodyHTML) > 200000 {
		return "", "", "", errors.New("provide email text of at most 100,000 bytes")
	}
	return subject, body, bodyHTML, nil
}

func htmlText(node *html.Node, depth int) (string, error) {
	if depth > 64 {
		return "", errors.New("email formatting is nested too deeply")
	}
	if node.Type == html.TextNode {
		return node.Data, nil
	}
	if node.Type == html.CommentNode {
		return "", nil
	}
	var text strings.Builder
	number := 1
	for _, attr := range node.Attr {
		if node.Data == "ol" && attr.Key == "start" {
			if start, err := strconv.Atoi(attr.Val); err == nil && start > 0 {
				number = start
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		value, err := htmlText(child, depth+1)
		if err != nil {
			return "", err
		}
		if child.Type == html.ElementNode && child.Data == "li" {
			prefix := "- "
			if node.Data == "ol" {
				prefix = fmt.Sprintf("%d. ", number)
				number++
			}
			text.WriteString(prefix + strings.ReplaceAll(strings.TrimSpace(value), "\n", "\n  ") + "\n")
		} else {
			text.WriteString(value)
		}
	}
	value := text.String()
	switch node.Data {
	case "br":
		return "\n", nil
	case "p":
		return value + "\n\n", nil
	case "ul", "ol":
		return strings.TrimRight(value, "\n") + "\n\n", nil
	case "blockquote":
		return "> " + strings.ReplaceAll(strings.TrimSpace(value), "\n", "\n> ") + "\n\n", nil
	case "a":
		for _, attr := range node.Attr {
			if attr.Key == "href" && strings.TrimSpace(value) != attr.Val {
				return value + " <" + attr.Val + ">", nil
			}
		}
	}
	return value, nil
}
