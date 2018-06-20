// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package props

import (
	"github.com/cockroachdb/cockroach/pkg/sql/opt"
)

// WeakKeys are combinations of columns that form a weak key. No two non-null
// rows are equal if they contain columns from a weak key. For more details, see
// LogicalProps.WeakKeys.
type WeakKeys []opt.ColSet

// ContainsSubsetOf returns true if the weak key list contains a key that is a
// subset of the given key. In that case, there's no reason to add the key to
// the list, since it's redundant.
func (wk WeakKeys) ContainsSubsetOf(weakKey opt.ColSet) bool {
	for _, existing := range wk {
		if existing.SubsetOf(weakKey) {
			return true
		}
	}
	return false
}

// Add appends a new weak key to the list of weak keys. It also ensures that no
// weak key is a superset of another, since that is a redundant weak key.
func (wk *WeakKeys) Add(new opt.ColSet) {
	// If one weak key is a subset of another, then use that, since the
	// longer key is redundant.
	insert := 0
	for i, existing := range *wk {
		// If new key is redundant, don't add it.
		if existing.SubsetOf(new) {
			return
		}

		// If existing key is redundant, then remove it from the list. Since
		// there may be multiple redundant keys, wait until after looping to
		// remove all at once.
		if !new.SubsetOf(existing) {
			if insert != i {
				(*wk)[insert] = existing
			}
			insert++
		}
	}
	*wk = append((*wk)[:insert], new)
}

// Copy returns a copy of the list of weak keys.
func (wk WeakKeys) Copy() WeakKeys {
	res := make(WeakKeys, len(wk))
	copy(res, wk)
	return res
}

// Combine combines this set of weak keys with the given set by constructing a
// new set and then adding keys from both sets to it, using the same semantics
// as the Add method.
func (wk WeakKeys) Combine(other WeakKeys) WeakKeys {
	if len(wk) == 0 {
		return other
	}
	if len(other) == 0 {
		return wk
	}
	res := make(WeakKeys, len(wk), len(wk)+len(other))
	copy(res, wk)
	for _, k := range other {
		res.Add(k)
	}
	return res
}