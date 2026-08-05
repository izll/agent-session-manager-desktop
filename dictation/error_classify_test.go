package dictation

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// INVALID_ARGUMENT is Google's catch-all for a malformed request, not a
// statement about credentials: "RecognitionAudio not set" — an audio problem —
// comes back under it. Both the REST and the streaming path once classified it
// as a dead key, so a user whose key was demonstrably valid (accepted over both
// REST and gRPC, and fine in the Cloud Console) was sent to check the key while
// the real cause stayed hidden behind a confident wrong answer.
//
// Asserts the mapping rather than the wording, so rephrasing a message does not
// break the test but re-widening the classification does.
func TestStreamFailureBlamesTheKeyOnlyWhenItIsTheKey(t *testing.T) {
	cases := []struct {
		name      string
		code      codes.Code
		detail    string
		wantTitle string
	}{
		{"expired or revoked key", codes.Unauthenticated, "unauthenticated", "api_key_invalid_title"},
		{"key lacks access", codes.PermissionDenied, "permission denied", "api_key_invalid_title"},
		{"quota gone", codes.ResourceExhausted, "quota exceeded", "quota_title"},

		// Both of these arrive as InvalidArgument, so the code alone cannot
		// separate them — only the message can. Getting either one wrong sends
		// the user looking in the wrong place.
		{"expired key, reported as InvalidArgument", codes.InvalidArgument,
			"API key expired. Please renew the API key.", "api_key_invalid_title"},
		{"malformed request", codes.InvalidArgument,
			"RecognitionAudio not set.", "stream_failed_title"},

		{"backend trouble", codes.Unavailable, "backend unavailable", "stream_failed_title"},
		{"deadline", codes.DeadlineExceeded, "deadline exceeded", "stream_failed_title"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotTitle string
			app := &AppService{onError: func(title, _ string) { gotTitle = title }}
			sr := &StreamingRecognizer{app: app}

			done := make(chan struct{})
			app.onError = func(title, _ string) { gotTitle = title; close(done) }
			sr.reportStreamFailure(status.Error(tc.code, tc.detail))
			<-done

			if gotTitle != tc.wantTitle {
				t.Errorf("%v reported as %q, want %q", tc.code, gotTitle, tc.wantTitle)
			}
		})
	}
}

// Stopping a recording cancels the stream every single time. Reporting that
// would put an error box on screen after every normal use.
func TestCancellationIsNotReported(t *testing.T) {
	reported := false
	app := &AppService{onError: func(_, _ string) { reported = true }}
	sr := &StreamingRecognizer{app: app}

	sr.reportStreamFailure(status.Error(codes.Canceled, "user stopped"))

	if reported {
		t.Error("a cancelled stream was reported as a failure")
	}
}
