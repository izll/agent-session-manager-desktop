package main

import "testing"

// Dictating into a form field with the buffer on gathers the words in the
// buffer, as for the terminal, so the panel can show them for correcting
// before they are sent into the field. Without the buffer they go straight in.
func TestFieldDictationGoesThroughTheBufferWhenItIsOn(t *testing.T) {
	d := NewDictationService()
	if got := d.fieldTargetHandler("streaming", true); got != d.bufferHandler {
		t.Errorf("streaming with the buffer on: %T, want the buffer", got)
	}
	if got := d.fieldTargetHandler("streaming", false); got != d.fieldHandler {
		t.Errorf("live preview: %T, want the field", got)
	}
	if got := d.fieldTargetHandler("free", true); got != d.fieldHandler {
		t.Errorf("not streaming: %T, want the field", got)
	}
}
