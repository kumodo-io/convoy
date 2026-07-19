/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package health

import (
	"errors"
	"testing"
	"time"
)

func TestDeriveState(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	deadline := 30 * time.Second

	t.Run("passed clears anchor", func(t *testing.T) {
		s, anchor := DeriveState(true, nil, now.Add(-time.Minute), &deadline, now)
		if s != StateHealthy || !anchor.IsZero() {
			t.Errorf("= %s,%v want Healthy, zero anchor", s, anchor)
		}
	})

	t.Run("eval error is unknown and preserves anchor", func(t *testing.T) {
		prev := now.Add(-time.Minute)
		s, anchor := DeriveState(false, errors.New("boom"), prev, &deadline, now)
		if s != StateUnknown || !anchor.Equal(prev) {
			t.Errorf("= %s,%v want Unknown, preserved anchor %v", s, anchor, prev)
		}
	})

	t.Run("first failure sets anchor and is progressing", func(t *testing.T) {
		s, anchor := DeriveState(false, nil, time.Time{}, &deadline, now)
		if s != StateProgressing || !anchor.Equal(now) {
			t.Errorf("= %s,%v want Progressing, anchor now", s, anchor)
		}
	})

	t.Run("failing past deadline is degraded", func(t *testing.T) {
		first := now.Add(-31 * time.Second)
		s, anchor := DeriveState(false, nil, first, &deadline, now)
		if s != StateDegraded || !anchor.Equal(first) {
			t.Errorf("= %s,%v want Degraded, kept anchor", s, anchor)
		}
	})

	t.Run("no deadline never escalates", func(t *testing.T) {
		first := now.Add(-time.Hour)
		s, anchor := DeriveState(false, nil, first, nil, now)
		if s != StateProgressing || !anchor.Equal(first) {
			t.Errorf("= %s,%v want Progressing (no deadline), kept anchor", s, anchor)
		}
	})
}
