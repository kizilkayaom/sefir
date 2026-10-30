package draftcontent

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestPrepareFormattedEmail(t *testing.T) {
	input := `<p>Hello <strong>Ömer</strong></p><ol start="3"><li><p>First</p></li><li><p>Second</p></li></ol><p><a href="https://example.com/work">My work</a></p>`
	subject, body, formatted, err := Prepare(" Subject ", "untrusted alternate text", input)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Subject" || body != "Hello Ömer\n\n3. First\n4. Second\n\nMy work <https://example.com/work>" {
		t.Fatalf("unexpected text: %q", body)
	}
	if !strings.Contains(formatted, "<strong>Ömer</strong>") || !strings.Contains(formatted, `start="3"`) {
		t.Fatal(formatted)
	}
	_, repeatedBody, repeatedHTML, err := Prepare(subject, body, formatted)
	if err != nil || repeatedBody != body || repeatedHTML != formatted {
		t.Fatal("formatting changed when preparing saved content")
	}
}

func TestPrepareRemovesUnsafeContent(t *testing.T) {
	input := `<p onclick="alert(1)" style="color:red">Hello <b>there</b><img src="https://tracker.example/pixel" onerror="alert(2)"></p><script>alert(3)</script><iframe src="https://other.example"></iframe><a href="javascript:alert(4)">bad link</a><a href="/api/logout">relative</a><a href="https://example.com">safe link</a>`
	_, body, formatted, err := Prepare("Subject", "", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"onclick", "style=", "<img", "<script", "<iframe", "javascript:", "alert(", `href="/api`} {
		if strings.Contains(formatted, forbidden) {
			t.Fatalf("unsafe content remained: %s", forbidden)
		}
	}
	if !strings.Contains(body, "Hello there") || !strings.Contains(formatted, `href="https://example.com"`) {
		t.Fatal("safe content was lost")
	}
	for _, input := range []string{"<p><br></p>", "<script>alert(1)</script>", strings.Repeat("<blockquote>", 70) + "text" + strings.Repeat("</blockquote>", 70)} {
		if _, _, _, err := Prepare("Subject", "", input); err == nil {
			t.Fatal("accepted empty or overly nested HTML")
		}
	}
}

func TestFormattingIsBoundToApprovalHash(t *testing.T) {
	recipient := "person@example.com"
	plain, err := Hash(&recipient, "Subject", "Body")
	if err != nil {
		t.Fatal(err)
	}
	legacy := sha256.Sum256([]byte(`{"recipient_email":"person@example.com","subject":"Subject","body":"Body"}`))
	if plain != fmt.Sprintf("%x", legacy) {
		t.Fatal("plain-text approval hashes changed")
	}
	bold, err := HashWithHTML(&recipient, "Subject", "Body", "<p><strong>Body</strong></p>")
	if err != nil {
		t.Fatal(err)
	}
	italic, err := HashWithHTML(&recipient, "Subject", "Body", "<p><em>Body</em></p>")
	if err != nil {
		t.Fatal(err)
	}
	if bold == italic || bold == plain {
		t.Fatal("formatting was not included in the approval hash")
	}
}
