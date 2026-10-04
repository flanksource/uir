package symbolhandle_test

import (
	"math"
	"sort"

	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// method is module 2, package 3, an exported method (kind 4), local 5: the bit fields written out by
// hand are 2<<52 | 3<<36 | 1<<35 | 4<<29 | 5.
var method = symbolhandle.Fields{Module: 2, Package: 3, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindMethod, Local: 5}

const methodHandle int64 = 0x0020_0038_8000_0005

var _ = Describe("H64b symbol handles", func() {
	It("splits the 63 value bits into module 11, package 16, visibility 1, kind 6, and local 29", func() {
		Expect([]uint{symbolhandle.ModuleBits, symbolhandle.PackageBits, 1, symbolhandle.KindBits, symbolhandle.LocalBits}).
			To(Equal([]uint{11, 16, 1, 6, 29}))
		Expect(symbolhandle.MaxLocal).To(Equal(uint64(1<<29 - 1)))
		Expect(symbolhandle.MaxKind).To(Equal(symbolhandle.Kind(63)))
	})

	DescribeTable("pack every field MSB-first into a non-negative int64",
		func(fields symbolhandle.Fields, expected int64) {
			handle, err := symbolhandle.Pack(fields)
			Expect(err).ToNot(HaveOccurred())
			Expect(handle).To(Equal(expected))
			unpacked, err := symbolhandle.Unpack(handle)
			Expect(err).ToNot(HaveOccurred())
			Expect(unpacked).To(Equal(fields))
		},
		Entry("the smallest handle, local 0 of the first kind", symbolhandle.Fields{Kind: symbolhandle.KindPackage}, int64(1<<29)),
		Entry("an exported method", method, methodHandle),
		Entry("the largest local", symbolhandle.Fields{Kind: symbolhandle.KindFunc, Local: 1<<29 - 1}, int64(3<<29|(1<<29-1))),
		Entry("the largest custom kind", symbolhandle.Fields{Kind: 63}, int64(63<<29)),
		Entry("every field at capacity", symbolhandle.Fields{
			Module: symbolhandle.MaxModule, Package: symbolhandle.MaxPackage, Visibility: symbolhandle.Exported,
			Kind: symbolhandle.MaxKind, Local: symbolhandle.MaxLocal,
		}, int64(math.MaxInt64)),
	)

	DescribeTable("reject a field that does not fit, naming it",
		func(fields symbolhandle.Fields, field string) {
			_, err := symbolhandle.Pack(fields)
			Expect(err).To(MatchError(ContainSubstring(field)))
		},
		Entry("module", symbolhandle.Fields{Module: symbolhandle.MaxModule + 1, Kind: symbolhandle.KindType}, "module 2048 exceeds 11 bits"),
		Entry("package", symbolhandle.Fields{Package: symbolhandle.MaxPackage + 1, Kind: symbolhandle.KindType}, "package 65536 exceeds 16 bits"),
		Entry("local", symbolhandle.Fields{Local: 1 << 29, Kind: symbolhandle.KindType}, "local 536870912 exceeds 29 bits"),
		Entry("kind", symbolhandle.Fields{Kind: 64}, "kind 64 exceeds 6 bits"),
		Entry("visibility", symbolhandle.Fields{Visibility: 2, Kind: symbolhandle.KindType}, "visibility 2 exceeds 1 bit"),
		Entry("the reserved kind 0", symbolhandle.Fields{Module: 2}, "kind 0 is reserved"),
	)

	It("rejects a negative handle", func() {
		_, err := symbolhandle.Unpack(-1)
		Expect(err).To(MatchError(ContainSubstring("sign bit")))
	})

	It("rejects a handle whose kind is the reserved code 0", func() {
		_, err := symbolhandle.Unpack(2<<52 | 5)
		Expect(err).To(MatchError(ContainSubstring("kind 0 is reserved")))
	})

	It("orders handles by module, package, visibility, kind, then local", func() {
		ordered := []symbolhandle.Fields{
			{Module: 0, Package: 9, Visibility: symbolhandle.Exported, Kind: symbolhandle.MaxKind, Local: 9},
			{Module: 1, Package: 0, Visibility: symbolhandle.Internal, Kind: symbolhandle.KindConst, Local: 9},
			{Module: 1, Package: 1, Visibility: symbolhandle.Internal, Kind: symbolhandle.KindConst, Local: 9},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindPackage, Local: 0},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindType, Local: 0},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindType, Local: 1},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.MaxKind, Local: 0},
		}
		handles := make([]int64, len(ordered))
		for i, fields := range ordered {
			var err error
			handles[i], err = symbolhandle.Pack(fields)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(sort.SliceIsSorted(handles, func(i, j int) bool { return handles[i] < handles[j] })).To(BeTrue(), "%x", handles)
	})

	It("rejects an unknown visibility by name", func() {
		_, err := symbolhandle.ParseVisibility("public")
		Expect(err).To(MatchError(ContainSubstring(`visibility "public"`)))
		exported, err := symbolhandle.ParseVisibility("exported")
		Expect(err).ToNot(HaveOccurred())
		Expect(exported).To(Equal(symbolhandle.Exported))
		Expect(symbolhandle.Internal.String()).To(Equal("internal"))
	})

	Describe("bucket ranges", func() {
		DescribeTable("span exactly one bucket prefix, inclusive",
			func(ranged func() (symbolhandle.Range, error), low, high int64) {
				bucket, err := ranged()
				Expect(err).ToNot(HaveOccurred())
				Expect(bucket).To(Equal(symbolhandle.Range{Low: low, High: high}))
				Expect(bucket.Contains(methodHandle)).To(BeTrue())
				Expect(bucket.Contains(low - 1)).To(BeFalse())
				Expect(bucket.Contains(high + 1)).To(BeFalse())
			},
			Entry("package", func() (symbolhandle.Range, error) { return symbolhandle.PackageRange(2, 3) },
				int64(0x0020_0030_0000_0000), int64(0x0020_003F_FFFF_FFFF)),
			Entry("package and visibility", func() (symbolhandle.Range, error) {
				return symbolhandle.VisibilityRange(2, 3, symbolhandle.Exported)
			}, int64(0x0020_0038_0000_0000), int64(0x0020_003F_FFFF_FFFF)),
			Entry("package, visibility, and kind", func() (symbolhandle.Range, error) {
				return symbolhandle.BucketRange(2, 3, symbolhandle.Exported, symbolhandle.KindMethod)
			}, int64(0x0020_0038_8000_0000), int64(0x0020_0038_9FFF_FFFF)),
		)

		It("gives the local within its bucket", func() {
			bucket, err := symbolhandle.BucketRange(method.Module, method.Package, method.Visibility, method.Kind)
			Expect(err).ToNot(HaveOccurred())
			Expect(methodHandle - bucket.Low).To(Equal(int64(method.Local)))
		})

		It("rejects a bucket outside the layout, naming the field", func() {
			_, err := symbolhandle.PackageRange(symbolhandle.MaxModule+1, 0)
			Expect(err).To(MatchError(ContainSubstring("module 2048 exceeds 11 bits")))
			_, err = symbolhandle.BucketRange(0, symbolhandle.MaxPackage+1, symbolhandle.Internal, symbolhandle.KindFunc)
			Expect(err).To(MatchError(ContainSubstring("package 65536 exceeds 16 bits")))
			_, err = symbolhandle.BucketRange(0, 0, symbolhandle.Internal, 0)
			Expect(err).To(MatchError(ContainSubstring("kind 0 is reserved")))
		})
	})

	It("reserves module numbers 0 for builtins and 1 for the standard library", func() {
		for key, expected := range map[string]uint64{"": 0, "std": 1} {
			number, reserved := symbolhandle.ReservedModuleNumber(key)
			Expect(reserved).To(BeTrue(), key)
			Expect(number).To(Equal(expected), key)
		}
		_, reserved := symbolhandle.ReservedModuleNumber("example.org/service")
		Expect(reserved).To(BeFalse())
		Expect(symbolhandle.FirstAllocatedModule).To(Equal(uint64(2)))
	})
})
