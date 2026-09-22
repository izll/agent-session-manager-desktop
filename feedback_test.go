package main

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// Feedback is composed into links rather than posted, so that nothing the app
// ships has to carry a credential. These cover what that composition has to
// get right.

func TestAReportBecomesBothLinks(t *testing.T) {
	app := &App{}
	links, err := app.ComposeFeedback(FeedbackReport{
		Kind:    "bug",
		Summary: "A fül nem mozdul",
		Detail:  "Elengedem, és semmi.",
	})
	if err != nil {
		t.Fatalf("composing: %v", err)
	}

	if !strings.HasPrefix(links.GitHub, feedbackRepo+"/issues/new?") {
		t.Errorf("GitHub link = %q", links.GitHub)
	}
	if !strings.HasPrefix(links.Email, "mailto:"+feedbackEmail) {
		t.Errorf("email link = %q", links.Email)
	}

	// The accented summary has to survive the trip, or the report arrives
	// mangled.
	parsed, err := url.Parse(links.GitHub)
	if err != nil {
		t.Fatalf("the GitHub link is not a URL: %v", err)
	}
	if got := parsed.Query().Get("title"); got != "A fül nem mozdul" {
		t.Errorf("title came through as %q", got)
	}
	if got := parsed.Query().Get("body"); !strings.Contains(got, "Elengedem") {
		t.Errorf("body came through as %q", got)
	}
}

// A mailto: body is not a query string. Some clients show the + that
// url.Values writes for a space as a literal plus, so the report arrives
// reading "Elengedem,+és+semmi".
func TestTheMailLinkEncodesSpacesForMailClients(t *testing.T) {
	app := &App{}
	links, err := app.ComposeFeedback(FeedbackReport{
		Summary: "két szó",
		Detail:  "három szó itt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(links.Email, "+") {
		t.Errorf("the mail link uses + for spaces, which some clients show "+
			"literally:\n  %s", links.Email)
	}
	if !strings.Contains(links.Email, "%20") {
		t.Errorf("spaces are not encoded at all: %s", links.Email)
	}
}

// The system line describes the user's machine, so it goes only when asked
// for.
func TestTheSystemLineIsOptional(t *testing.T) {
	app := &App{}

	without, _ := app.ComposeFeedback(FeedbackReport{Detail: "no system line"})
	if strings.Contains(without.Text, "asmgr-desktop ") {
		t.Errorf("the system line was attached without being asked for:\n%s",
			without.Text)
	}

	with, _ := app.ComposeFeedback(FeedbackReport{
		Detail:        "with system line",
		IncludeSystem: true,
	})
	if !strings.Contains(with.Text, "asmgr-desktop ") {
		t.Errorf("the system line is missing when asked for:\n%s", with.Text)
	}
	// After the report, not before: what the person wrote comes first.
	if strings.Index(with.Text, "with system line") > strings.Index(with.Text, "asmgr-desktop ") {
		t.Error("the system line comes before what the user wrote")
	}
}

// Browsers and mail clients cut a long URL silently, losing the end of the
// report. It is cut here instead, where the dialog can say so — and the
// clipboard copy is never cut, so nothing is actually lost.
func TestALongReportIsCutForTheLinksOnly(t *testing.T) {
	app := &App{}
	long := strings.Repeat("hosszú szöveg ", 400)

	links, err := app.ComposeFeedback(FeedbackReport{Summary: "sok", Detail: long})
	if err != nil {
		t.Fatal(err)
	}
	if !links.Truncated {
		t.Error("a report far past the URL limit was not reported as cut")
	}

	parsed, _ := url.Parse(links.GitHub)
	body := parsed.Query().Get("body")
	if len(body) > maxFeedbackURLBytes {
		t.Errorf("the link body is %d bytes, past the limit", len(body))
	}

	// Cut on a rune boundary: half a character is neither readable nor valid
	// UTF-8 at the other end.
	if !utf8.ValidString(body) {
		t.Error("the body was cut mid-character")
	}

	// The clipboard copy keeps everything.
	if len(links.Text) < len(long) {
		t.Error("the clipboard copy was cut too, so the report is lost")
	}
}

// A report with no summary still has to produce a usable link rather than an
// issue titled with an empty string.
func TestAnEmptyReportStillComposes(t *testing.T) {
	app := &App{}
	links, err := app.ComposeFeedback(FeedbackReport{})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(links.GitHub)
	if err != nil {
		t.Fatalf("not a URL: %v", err)
	}
	if parsed.Query().Get("title") == "" {
		t.Error("the issue would be opened with no title at all")
	}
}

// Nothing here may post anywhere: that is the whole reason this composes links
// instead. A credential shipped in the binary can be read out of it — an ntfy
// topic is the plainest case, since it is often the only thing protecting a
// channel, and shipping one lets a stranger both flood the author's phone and
// read everything users send.
func TestFeedbackPostsNothingAndShipsNoCredential(t *testing.T) {
	// Code only. The comments in that file explain why none of this is used,
	// and matching on them would flag the explanation as the offence.
	source := codeWithoutComments(readSourceFile(t, "feedback.go"))

	for _, forbidden := range []string{
		"http.Post", "http.NewRequest", "smtp.", "ntfy",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("feedback.go uses %s: sending from the app needs a "+
				"credential in the binary, which anybody can read out of it",
				forbidden)
		}
	}
}

// codeWithoutComments strips // comments, so a rule about what the code does
// is not tripped by prose about what it deliberately does not.
func codeWithoutComments(source string) string {
	var out strings.Builder
	for _, line := range strings.Split(source, "\n") {
		if cut := strings.Index(line, "//"); cut >= 0 {
			line = line[:cut]
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}
