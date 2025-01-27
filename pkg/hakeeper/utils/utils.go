// Copyright 2022 MatrixOrigin.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import "time"

const (
	TickPerSecond = 10
	StoreTimeout  = 10 * time.Minute
)

func ExpiredTick(start uint64, timeout time.Duration) uint64 {
	return uint64(timeout/time.Second)*TickPerSecond + start
}
