package main

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
)

// Sending feedback without putting a secret in the binary.
//
// Every route that posts straight to a service needs a credential shipped with
// the app — an SMTP password, an API token, an ntfy topic — and a credential in
// a binary is a credential anybody can read out of it. An ntfy topic is the
// clearest case: it is often the only thing protecting a push channel, so
// shipping one lets a stranger both flood the author's phone and subscribe to
// everything users report.
//
// So nothing is posted from here. A report is composed into a link and handed
// to the host: GitHub through the browser, where the user's own account signs
// it, or the user's own mail client. Both are unauthenticated from the app's
// side, which is the point.

// feedbackRepo is where issues are opened.
const feedbackRepo = "https://github.com/izll/agent-session-manager-desktop"

// feedbackEmail receives reports from anyone who would rather not use GitHub.
const feedbackEmail = "antalizn@gmail.com"

// maxFeedbackURLBytes bounds what is put in a link.
//
// A URL has no standard limit, but browsers and mail clients impose their own
// — around 2000 characters is the safe figure, and something above it may be
// silently truncated, losing the end of the report. The text is cut here
// instead, where it can be said plainly.
const maxFeedbackURLBytes = 1800

// FeedbackReport is what the dialog collected.
type FeedbackReport struct {
	// Kind is "bug" or "idea", which decides the title and the label.
	Kind string `json:"kind"`
	// Summary is the one-line title.
	Summary string `json:"summary"`
	// Detail is the body: what happened, what was expected.
	Detail string `json:"detail"`
	// IncludeSystem attaches the version and platform. Asked rather than
	// assumed: it is the user's machine being described.
	IncludeSystem bool `json:"includeSystem"`
}

// FeedbackLinks are the ways one report can be sent.
type FeedbackLinks struct {
	// GitHub opens a new issue with the fields already filled in.
	GitHub string `json:"github"`
	// Email opens the user's mail client, addressed and composed.
	Email string `json:"email"`
	// Text is the report as plain text, for the clipboard — the way out for
	// someone who has neither a GitHub account nor a mail client set up.
	Text string `json:"text"`
	// Truncated says the report was too long for a link and was cut. The
	// clipboard text is never cut, so the dialog can point at it.
	Truncated bool `json:"truncated"`
}

// ComposeFeedback turns a report into the links that can send it.
//
// Nothing is sent here, and nothing is stored: this only builds text. The user
// sees what they are about to send in their browser or mail client, and can
// change it there before it goes anywhere.
func (a *App) ComposeFeedback(report FeedbackReport) (*FeedbackLinks, error) {
	title := strings.TrimSpace(report.Summary)
	if title == "" {
		title = "Feedback"
	}

	body := a.feedbackBody(report)
	links := &FeedbackLinks{Text: title + "\n\n" + body}

	// Cut for the links only. The clipboard copy keeps the whole thing, so a
	// long report is never lost — it just has to travel that way.
	linkBody := body
	if len(linkBody) > maxFeedbackURLBytes {
		linkBody = linkBody[:maxFeedbackURLBytes]
		// Not mid-character: a URL-escaped half rune is neither readable nor
		// valid UTF-8 at the other end.
		for len(linkBody) > 0 && !isRuneStart(linkBody[len(linkBody)-1]) {
			linkBody = linkBody[:len(linkBody)-1]
		}
		links.Truncated = true
	}

	issue := url.Values{}
	issue.Set("title", title)
	issue.Set("body", linkBody)
	if label := feedbackLabel(report.Kind); label != "" {
		issue.Set("labels", label)
	}
	links.GitHub = feedbackRepo + "/issues/new?" + issue.Encode()

	mail := url.Values{}
	mail.Set("subject", "asmgr-desktop: "+title)
	mail.Set("body", linkBody)
	// Encode uses + for spaces, which is right for a query string and wrong
	// for mailto: some clients show the pluses literally.
	links.Email = "mailto:" + feedbackEmail + "?" +
		strings.ReplaceAll(mail.Encode(), "+", "%20")

	return links, nil
}

// feedbackBody assembles what the report says.
func (a *App) feedbackBody(report FeedbackReport) string {
	var out strings.Builder
	out.WriteString(strings.TrimSpace(report.Detail))

	if report.IncludeSystem {
		// Last, so the part a person wrote comes first, and separated so it
		// reads as an attachment rather than as part of their sentence.
		out.WriteString("\n\n---\n")
		out.WriteString(fmt.Sprintf("asmgr-desktop %s · %s/%s",
			a.GetVersion(), runtime.GOOS, runtime.GOARCH))
	}
	return out.String()
}

func feedbackLabel(kind string) string {
	switch kind {
	case "bug":
		return "bug"
	case "idea":
		return "enhancement"
	default:
		return ""
	}
}

// isRuneStart reports whether a byte begins a UTF-8 rune, as opposed to
// continuing one.
func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
