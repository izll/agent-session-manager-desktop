package main

import "fmt"

// The order of the view bar's views — terminal, notes, tasks, files, diff.
//
// Set for every tab in Settings, and per tab from the tab's menu, where an
// empty order goes back to following the global one. Only the order is kept:
// which views exist is the app's, so the frontend fills in any view a stored
// order lacks, and one added in a later version still shows up.

// viewIDs are the views an order may name.
var viewIDs = map[string]bool{"terminal": true, "notes": true, "tasks": true, "browser": true, "diff": true}

// checkViewOrder refuses an order with a view the app does not have, or one
// named twice.
func checkViewOrder(order []string) error {
	seen := make(map[string]bool, len(order))
	for _, view := range order {
		if !viewIDs[view] {
			return fmt.Errorf("unknown view %q", view)
		}
		if seen[view] {
			return fmt.Errorf("view %q named twice", view)
		}
		seen[view] = true
	}
	return nil
}

// validViewOrder is the order as stored from Settings: an invalid one is
// dropped rather than failing the whole save, and the default takes over.
func validViewOrder(order []string) []string {
	if len(order) == 0 || checkViewOrder(order) != nil {
		return nil
	}
	return order
}

// SetTabViewOrder sets one tab's view bar order; an empty order follows the
// global one again.
func (a *App) SetTabViewOrder(sessionID string, windowIdx int, order []string, expectedProjectID string) error {
	if err := checkViewOrder(order); err != nil {
		return err
	}
	if len(order) == 0 {
		order = nil
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		inst.ViewOrder = order
		return a.storage.UpdateInstance(inst)
	}
	for i := range inst.FollowedWindows {
		if inst.FollowedWindows[i].Index == windowIdx {
			inst.FollowedWindows[i].ViewOrder = order
			return a.storage.UpdateInstance(inst)
		}
	}
	return fmt.Errorf("window %d not found", windowIdx)
}
