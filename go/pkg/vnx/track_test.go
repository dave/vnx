package vnx

import "testing"

const twoSegmentTrack = `{"vnx":"0.11.0","folders":[{"id":"cairngorms"}],"tracks":[{
  "folder":"cairngorms","name":"Lairig Ghru, 12 May",
  "started":"2026-05-12T07:03:11Z","ended":"2026-05-12T16:41:02Z",
  "segments":[
    {"pips":[[-3.6712,57.1180,341,1778569391000],
             [-3.6699,57.1158,352.4,1778569442000],
             [-3.6681,57.1139,366.9,1778569494000]]},
    {"pips":[[-3.6644,57.1085,452,1778571280000],
             [-3.6605,57.1042,513.1,1778571344000]]}
  ]}]}`

func TestTrackRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(twoSegmentTrack))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean track produced %v", diags)
	}
	tr := doc.Tracks[0]
	if len(tr.Segments) != 2 || len(tr.Segments[0].Pips) != 3 || len(tr.Segments[1].Pips) != 2 {
		t.Fatalf("segments wrong: %+v", tr.Segments)
	}
	if tr.Started == "" || tr.Ended == "" {
		t.Fatal("started/ended must be carried")
	}
}

// §3.4. Moving time is the sum over segments of (last pip time - first pip
// time). It is deliberately NOT the wall-clock span, which the pause between
// segments makes longer. Verified against the app: movingTime sums per-leg
// last-minus-first, legs map 1:1 to segments, and no motion threshold exists.
func TestMovingTimeExcludesThePause(t *testing.T) {
	doc, _, err := Decode([]byte(twoSegmentTrack))
	if err != nil {
		t.Fatal(err)
	}
	tr := doc.Tracks[0]
	moving, known := tr.MovingTime()
	if !known {
		t.Fatal("a track whose pips carry time has a known moving time")
	}
	// segment 1: 1778569494000-1778569391000 = 103 s; segment 2: 64 s.
	if moving.Seconds() != 167 {
		t.Fatalf("moving time = %v, want 167s", moving)
	}
	started, err := tr.Started.Time()
	if err != nil {
		t.Fatal(err)
	}
	ended, err := tr.Ended.Time()
	if err != nil {
		t.Fatal(err)
	}
	wall := ended.Sub(started)
	if moving >= wall {
		t.Fatalf("moving time %v must be less than the wall clock %v — the pause is not geometry", moving, wall)
	}
}

// ⚠ The derivation has a hole and it must not be papered over: a GPX import
// without timestamps produces a track with no pip times at all. The answer is
// an explicit UNKNOWN, never zero. Zero moving time is a plausible-looking
// wrong answer, which is worse than an absent one.
func TestTimelessTrackReportsUnknownNotZero(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.11.0","tracks":[{"name":"imported",
	  "segments":[{"pips":[[-3.67,57.11,340],[-3.66,57.10,350]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	moving, known := doc.Tracks[0].MovingTime()
	if known {
		t.Fatalf("a track with no pip times has no known moving time, got %v", moving)
	}
	if moving != 0 {
		t.Fatalf("the unknown case must not carry a made-up duration, got %v", moving)
	}
}

// A single-pip segment has no span. It contributes nothing rather than being an
// error — a recording can legitimately hold one fix before a pause.
func TestSinglePipSegmentContributesNothing(t *testing.T) {
	doc, _, err := Decode([]byte(`{"vnx":"0.11.0","tracks":[{"name":"t","segments":[
	  {"pips":[[-3.67,57.11,340,1000]]},
	  {"pips":[[-3.66,57.10,350,5000],[-3.65,57.09,360,9000]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	moving, known := doc.Tracks[0].MovingTime()
	if !known || moving.Seconds() != 4 {
		t.Fatalf("got %v (known=%v), want 4s", moving, known)
	}
}

// The gap between segment k and k+1 is a pause. It is never geometry and must
// never be walked — so segments stay separate and are not concatenated.
func TestSegmentsAreNotConcatenated(t *testing.T) {
	doc, _, err := Decode([]byte(twoSegmentTrack))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Tracks[0].Segments) != 2 {
		t.Fatal("segments must stay separate; the gap between them is a pause")
	}
}

func TestTrackWithBadTimestampIsDiagnosedNotDropped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","tracks":[{"name":"t",
	  "started":"2026-05-12T07:03:11+01:00",
	  "segments":[{"pips":[[-3.67,57.11]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Tracks) != 1 {
		t.Fatal("a bad timestamp drops the timestamp, not the track and its geometry")
	}
	if doc.Tracks[0].Started != "" {
		t.Fatal("the unusable timestamp must not be kept")
	}
	if len(diags) != 1 || diags[0].Path != "/tracks/0/started" {
		t.Fatalf("got %v", diags)
	}
}
