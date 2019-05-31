// Copyright 2019 The Cockroach Authors.
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

package raft

import (
	"sort"
)

type MajorityConfig map[uint64]struct{}

// func VoteResult(n int, votes map[uint64]bool) voteResult {
// 	m := map[uint64]uint64{}
// 	for nodeID, vote := range votes {
// 		if !vote {
// 			m[nodeID] = 0
// 		} else {
// 			m[nodeID] = 1
// 		}
// 	}
// 	idx, final := majorityCommittedIdx(n, m)
// 	if !final {
// 		return voteOpen
// 	}
// 	if idx == 1 {
// 		return voteWon
// 	}
// 	return voteLost
// }

// metamorphic testing: commit idx invariant under adding n members to the quorum
// and n/2 copies of the commit index
// dumb implementation that uses counting maps idx -> votes + quickcheck

// majorityCommittedIdx computes the largest index that has been acknowledged by
// a majority of voters.

func (c MajorityConfig) CommittedIndex(l IndexLookuper) (committedIdx uint64, final bool) {
	n := len(c)
	if n == 0 {
		return 0, false
	}

	var scratchBuf struct {
		// TODO(tbg): sadly, sort.Slice causes scratchBuf to escape to the heap
		// at the time of writing. This may change in go 1.13. Or we generate
		// a family of bespoke sorters for slices of length <= 5.
		//
		// See: https://github.com/golang/go/issues/17332
		//
		// goescape.Stack
		sl [5]uint64
	}

	srt := uint64Slice(scratchBuf.sl[:])
	if cap(srt) < n {
		srt = make(uint64Slice, n)
	}
	srt = srt[:n]

	var votesCast int
	{
		i := n - 1
		for id := range c {
			if idx, ok := l.Index(id); ok {
				srt[i] = idx
				i--
			}
		}
		votesCast = (n - 1) - i
	}

	sort.Slice(srt, func(i, j int) bool { return srt[i] < srt[j] })
	pos := n - (n/2 + 1)

	return srt[pos], votesCast > pos && srt[pos+n-votesCast] == srt[pos]
}

type IndexLookuper interface {
	Index(voterID uint64) (idx uint64, found bool)
}

// metamorphic: add bogus voter to nToIdx
func dumbMajorityCommittedIdx(voters map[uint64]struct{}, l IndexLookuper) (_committedIdx uint64, _malleable bool) {
	idToIdx := map[uint64]uint64{}
	for id := range voters {
		if idx, ok := l.Index(id); ok {
			idToIdx[id] = idx
		}
	}

	pendingVotes := len(voters) - len(idToIdx)

	// Build a map from index to voters who have acked that or any higher index.
	idxToVotes := map[uint64]int{}
	for _, idx := range idToIdx {
		idxToVotes[idx] = 0
	}

	for _, idx := range idToIdx {
		for idy := range idxToVotes {
			if idy > idx {
				continue
			}
			idxToVotes[idy]++
		}
	}

	// Find the maximum index that has achieved quorum.
	q := len(voters)/2 + 1
	var maxQuorumIdx uint64
	for idx, n := range idxToVotes {
		if n >= q && idx > maxQuorumIdx {
			maxQuorumIdx = idx
		}
	}

	// Check whether there's a higher index that could receive
	// more votes, reach a quorum, and thus increase maxQuorumIdx
	// in the future.
	var malleable bool
	for idx, n := range idxToVotes {
		if idx <= maxQuorumIdx {
			continue
		}
		if n+pendingVotes >= q {
			malleable = true
			break
		}
	}

	return maxQuorumIdx, malleable
}
