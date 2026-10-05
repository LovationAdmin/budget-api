package handlers

import (
	"context"
	"errors"
	"testing"
)

func TestBulkRegistry_DedupesSameAnalysisAndSupersedesOthers(t *testing.T) {
	r := &bulkRegistry{runs: map[string]*bulkRun{}}

	ctx1, finish1, running := r.start("b1", "sig-A", false)
	if running || ctx1 == nil {
		t.Fatal("first run should start")
	}
	if _, _, running := r.start("b1", "sig-A", false); !running {
		t.Fatal("the same analysis must not start twice")
	}

	ctx2, finish2, running := r.start("b1", "sig-B", false)
	if running {
		t.Fatal("a different analysis should start")
	}
	if !errors.Is(ctx1.Err(), context.Canceled) {
		t.Fatal("the stale run must be cancelled")
	}

	finish1() // the stale run ending must not unregister the new one
	if _, _, running := r.start("b1", "sig-B", false); !running {
		t.Fatal("the new run must still be registered")
	}
	ctx3, finish3, running := r.start("b1", "sig-B", true)
	if running || !errors.Is(ctx2.Err(), context.Canceled) {
		t.Fatal("force must restart a running analysis")
	}
	finish2() // the restarted run ending must not unregister the new one
	if _, _, running := r.start("b1", "sig-B", false); !running {
		t.Fatal("the forced run must still be registered")
	}
	finish3()
	if ctx3.Err() == nil {
		t.Fatal("finish must release the run's context")
	}
	if _, _, running := r.start("b1", "sig-B", false); running {
		t.Fatal("a finished analysis can run again")
	}
}
