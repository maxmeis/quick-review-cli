package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"quick-review-cli/internal/domain"
)

func TestDismissedDialogIgnoresQueuedStateUpdates(t *testing.T) {
	for _, lifecycle := range []bool{true, false} {
		var actions []domain.Action
		m := NewModel(domain.State{}, func(a domain.Action) { actions = append(actions, a) }).(model)
		s := testState()
		s.Questions = nil
		s.Snapshot.State = "CLOSED"
		s.ClosedPrompt = lifecycle
		s.QuitRequested = !lifecycle
		m = apply(m, stateMsg(s))
		if !m.quitDialog {
			t.Fatal("initial prompt missing")
		}
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
		// This update was queued before the controller processed cancel-quit.
		s.Phase = "Reviewing"
		m = apply(m, stateMsg(s))
		if m.quitDialog || !m.suppressQuit {
			t.Fatal("queued update reopened dismissed dialog")
		}
		// The acknowledgement ends suppression without reopening the prompt.
		s.ClosedPrompt = false
		s.QuitRequested = false
		m = apply(m, stateMsg(s))
		if m.quitDialog || m.suppressQuit {
			t.Fatal("cancel acknowledgement did not clear prompt")
		}
		m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlP})
		for _, r := range "refresh" {
			m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
		if len(actions) != 2 || actions[0].Kind != "cancel-quit" || actions[1].Kind != "refresh" {
			t.Fatalf("unexpected actions: %+v", actions)
		}
	}
}

func TestNewLifecyclePromptSurvivesCoalescedCancellation(t *testing.T) {
	m := NewModel(domain.State{}, nil).(model)
	s := testState()
	s.Snapshot.State = "CLOSED"
	s.ClosedPrompt = true
	m = apply(m, stateMsg(s))
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	// Controller updates can coalesce the cancellation acknowledgement with merge.
	s.Snapshot.State = "MERGED"
	m = apply(m, stateMsg(s))
	if !m.quitDialog || m.suppressQuit {
		t.Fatal("new merged prompt was suppressed")
	}
}

func TestCancellationAckPreservesNewLocalQuitDialog(t *testing.T) {
	m := NewModel(testState(), nil).(model)
	m.active = reportTab
	s := testState()
	s.Snapshot.State = "CLOSED"
	s.ClosedPrompt = true
	m = apply(m, stateMsg(s))
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !m.quitDialog {
		t.Fatal("local quit dialog missing")
	}
	s.ClosedPrompt = false
	m = apply(m, stateMsg(s))
	if !m.quitDialog || m.suppressQuit {
		t.Fatal("old cancellation erased new local quit dialog")
	}
}
