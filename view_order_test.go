package main

import (
	"reflect"
	"testing"

	"asmgr-desktop/session"
)

func TestATabKeepsAViewOrderOfItsOwnUntilReset(t *testing.T) {
	app := checkpointTestApp(t, &session.Instance{ID: "vo", Name: "vo", Path: t.TempDir(), Status: session.StatusStopped,
		FollowedWindows: []session.FollowedWindow{{ID: "t1", Index: 1, Name: "tab", Agent: session.AgentTerminal}}})
	project := app.storage.GetActiveProjectID()
	order := []string{"diff", "terminal", "notes"}

	for _, window := range []int{0, 1} {
		if err := app.SetTabViewOrder("vo", window, order, project); err != nil {
			t.Fatalf("window %d: %v", window, err)
		}
	}
	inst, err := app.storage.GetInstance("vo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inst.ViewOrder, order) || !reflect.DeepEqual(inst.FollowedWindows[0].ViewOrder, order) {
		t.Errorf("stored %v and %v, want %v for both", inst.ViewOrder, inst.FollowedWindows[0].ViewOrder, order)
	}
	if info := app.instanceToSessionInfo(inst); !reflect.DeepEqual(info.ViewOrder, order) {
		t.Errorf("the frontend is told %v", info.ViewOrder)
	}

	// Back to the global order.
	if err := app.SetTabViewOrder("vo", 1, nil, project); err != nil {
		t.Fatal(err)
	}
	inst, _ = app.storage.GetInstance("vo")
	if inst.FollowedWindows[0].ViewOrder != nil || inst.ViewOrder == nil {
		t.Errorf("reset the tab: tab %v, main %v", inst.FollowedWindows[0].ViewOrder, inst.ViewOrder)
	}
}

func TestAViewOrderNamesOnlyViewsTheAppHas(t *testing.T) {
	app := checkpointTestApp(t, &session.Instance{ID: "vo", Name: "vo", Path: t.TempDir(), Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()
	for _, order := range [][]string{{"terminal", "inbox"}, {"notes", "notes"}} {
		if err := app.SetTabViewOrder("vo", 0, order, project); err == nil {
			t.Errorf("%v was accepted", order)
		}
	}
	if err := app.SetTabViewOrder("vo", 9, []string{"notes"}, project); err == nil {
		t.Error("a tab that does not exist was given an order")
	}
}

func TestTheGlobalViewOrderIsSaved(t *testing.T) {
	app := checkpointTestApp(t, &session.Instance{ID: "vo", Name: "vo", Path: t.TempDir(), Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()
	settings, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}

	settings.ViewOrder = []string{"browser", "terminal"}
	if err := app.SaveSettings(*settings, project); err != nil {
		t.Fatal(err)
	}
	got, _ := app.GetSettings()
	if !reflect.DeepEqual(got.ViewOrder, []string{"browser", "terminal"}) {
		t.Errorf("saved %v", got.ViewOrder)
	}

	// One the app could not show is dropped, not saved.
	settings.ViewOrder = []string{"browser", "browser"}
	if err := app.SaveSettings(*settings, project); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.GetSettings(); got.ViewOrder != nil {
		t.Errorf("an invalid order was kept: %v", got.ViewOrder)
	}
}

func TestWhatADragChangesIsSaved(t *testing.T) {
	app := checkpointTestApp(t, &session.Instance{ID: "vo", Name: "vo", Path: t.TempDir(), Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()
	settings, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ViewOrderDrag != "" {
		t.Fatalf("default = %q, want asking", settings.ViewOrderDrag)
	}
	for _, target := range []string{"tab", "global", ""} {
		settings.ViewOrderDrag = target
		if err := app.SaveSettings(*settings, project); err != nil {
			t.Fatal(err)
		}
		if got, _ := app.GetSettings(); got.ViewOrderDrag != target {
			t.Errorf("saved %q, read back %q", target, got.ViewOrderDrag)
		}
	}
	settings.ViewOrderDrag = "everywhere"
	if err := app.SaveSettings(*settings, project); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.GetSettings(); got.ViewOrderDrag != "" {
		t.Errorf("an unknown target was kept: %q", got.ViewOrderDrag)
	}
}
