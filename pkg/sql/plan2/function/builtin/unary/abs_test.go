package unary

import (
	"errors"
	"log"
	"testing"

	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/sql/testutil"
	"github.com/matrixorigin/matrixone/pkg/vm/mheap"
	"github.com/matrixorigin/matrixone/pkg/vm/mmu/guest"
	"github.com/matrixorigin/matrixone/pkg/vm/mmu/host"
	"github.com/matrixorigin/matrixone/pkg/vm/process"
	"github.com/smartystreets/goconvey/convey"
)

func Test_AbsUint64(t *testing.T) {
	convey.Convey("Test abs for uint64 succ", t, func() {
		var uint64VecBase = []uint64{1, 0}
		var nsp1 []uint64 = []uint64{2}
		var origVecs = make([]*vector.Vector, 1)
		var proc = process.New(mheap.New(&guest.Mmu{Mmu: host.New(100000), Limit: 100000}))
		origVecs[0] = testutil.MakeUint64Vector(uint64VecBase, nsp1)
		vec, err := AbsUInt64(origVecs, proc)
		if err != nil {
			log.Fatal(err)
		}
		data, ok := vec.Col.([]uint64)
		if !ok {
			log.Fatal(errors.New("the AbsUint64 function return value type is not []uint6"))
		}
		compVec := []uint64{1, 0}
		compNsp := []int64{2}

		for i := 0; i < len(compVec); i++ {
			convey.So(data[i], convey.ShouldEqual, compVec[i])
		}
		j := 0
		for i := 0; i < len(compVec); i++ {
			if j < len(compNsp) {
				if compNsp[j] == int64(i) {
					convey.So(vec.Nsp.Np.Contains(uint64(i)), convey.ShouldBeTrue)
					j++
				} else {
					convey.So(vec.Nsp.Np.Contains(uint64(i)), convey.ShouldBeFalse)
				}
			} else {
				convey.So(vec.Nsp.Np.Contains(uint64(i)), convey.ShouldBeFalse)
			}
		}

	})
}
