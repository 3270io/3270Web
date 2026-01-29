package main

import (
"testing"

"github.com/jnnngs/3270Web/internal/session"
)

func TestStopWorkflowPlaybackUpdatesState(t *testing.T) {
sess := &session.Session{Playback: &session.WorkflowPlayback{Active: true, Paused: true}}

stopWorkflowPlayback(sess)

var stopRequested, stepRequested, paused bool
var events []session.WorkflowEvent
withSessionLock(sess, func() {
if sess.Playback != nil {
stopRequested = sess.Playback.StopRequested
stepRequested = sess.Playback.StepRequested
paused = sess.Playback.Paused
}
events = append([]session.WorkflowEvent(nil), sess.PlaybackEvents...)
})

if !stopRequested {
t.Fatalf("expected StopRequested to be true")
}
if !stepRequested {
t.Fatalf("expected StepRequested to be true")
}
if paused {
t.Fatalf("expected Paused to be false")
}
if len(events) == 0 {
t.Fatalf("expected playback events to include stop message")
}
if got := events[len(events)-1].Message; got != "Playback stop requested" {
t.Fatalf("expected stop event message, got %q", got)
}
}
