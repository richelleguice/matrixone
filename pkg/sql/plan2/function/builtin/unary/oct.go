// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package unary

import (
	"github.com/matrixorigin/matrixone/pkg/container/nulls"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/vectorize/oct"
	"github.com/matrixorigin/matrixone/pkg/vm/process"
	"golang.org/x/exp/constraints"
)

func Oct[T constraints.Unsigned | constraints.Signed](vectors []*vector.Vector, proc *process.Process) (*vector.Vector, error) {
	inputVector := vectors[0]
	resultType := types.Type{Oid: types.T_varchar, Size: 24}
	resultElementSize := int(resultType.Size)
	if inputVector.IsScalar() {
		if inputVector.ConstVectorIsNull() {
			return proc.AllocScalarNullVector(resultType), nil
		}
		inputValues := inputVector.Col.([]T)
		resultVector := vector.NewConst(resultType)
		resultValues := &types.Bytes{
			Data:    []byte{},
			Offsets: make([]uint32, 1),
			Lengths: make([]uint32, 1),
		}
		vector.SetCol(resultVector, oct.Oct(inputValues, resultValues))
		return resultVector, nil
	} else {
		inputValues := inputVector.Col.([]T)
		resultVector, err := proc.AllocVector(resultType, int64(resultElementSize*len(inputValues)))
		if err != nil {
			return nil, err
		}
		resultValues := &types.Bytes{
			Data:    []byte{},
			Offsets: make([]uint32, 1),
			Lengths: make([]uint32, 1),
		}
		nulls.Set(resultVector.Nsp, inputVector.Nsp)
		vector.SetCol(resultVector, oct.Oct(inputValues, resultValues))
		return resultVector, nil
	}
}
