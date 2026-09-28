package symbolhandle_test

import (
	"math"
	"sort"

	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// method is module 2, package 3, an exported method, local 5: the bit fields written out by hand are
// 2<<52 | 3<<36 | 1<<35 | 3<<32 | 5.
var method = symbolhandle.Fields{Module: 2, Package: 3, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindMethod, Local: 5}

const methodHandle int64 = 0x0020_003B_0000_0005

var _ = Describe("H64a symbol handles", func() {
	DescribeTable("pack every field MSB-first into a non-negative int64",
		func(fields symbolhandle.Fields, expected int64) {
			handle, err := symbolhandle.Pack(fields)
			Expect(err).ToNot(HaveOccurred())
			Expect(handle).To(Equal(expected))
			unpacked, err := symbolhandle.Unpack(handle)
			Expect(err).ToNot(HaveOccurred())
			Expect(unpacked).To(Equal(fields))
		},
		Entry("the zero handle", symbolhandle.Fields{}, int64(0)),
		Entry("an exported method", method, methodHandle),
		Entry("every field at capacity", symbolhandle.Fields{
			Module: symbolhandle.MaxModule, Package: symbolhandle.MaxPackage, Visibility: symbolhandle.Exported,
			Kind: symbolhandle.KindBuiltin, Local: symbolhandle.MaxLocal,
		}, int64(math.MaxInt64)),
	)

	DescribeTable("reject a field that does not fit, naming it",
		func(fields symbolhandle.Fields, field string) {
			_, err := symbolhandle.Pack(fields)
			Expect(err).To(MatchError(ContainSubstring(field)))
		},
		Entry("module", symbolhandle.Fields{Module: symbolhandle.MaxModule + 1}, "module 2048 exceeds 11 bits"),
		Entry("package", symbolhandle.Fields{Package: symbolhandle.MaxPackage + 1}, "package 65536 exceeds 16 bits"),
		Entry("local", symbolhandle.Fields{Local: symbolhandle.MaxLocal + 1}, "local 4294967296 exceeds 32 bits"),
		Entry("kind", symbolhandle.Fields{Kind: 8}, "kind 8 exceeds 3 bits"),
		Entry("visibility", symbolhandle.Fields{Visibility: 2}, "visibility 2 exceeds 1 bit"),
	)

	It("rejects a negative handle", func() {
		_, err := symbolhandle.Unpack(-1)
		Expect(err).To(MatchError(ContainSubstring("sign bit")))
	})

	It("orders handles by module, package, visibility, kind, then local", func() {
		ordered := []symbolhandle.Fields{
			{Module: 0, Package: 9, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindBuiltin, Local: 9},
			{Module: 1, Package: 0, Visibility: symbolhandle.Internal, Kind: symbolhandle.KindConst, Local: 9},
			{Module: 1, Package: 1, Visibility: symbolhandle.Internal, Kind: symbolhandle.KindConst, Local: 9},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindPackage, Local: 0},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindType, Local: 0},
			{Module: 1, Package: 1, Visibility: symbolhandle.Exported, Kind: symbolhandle.KindType, Local: 1},
		}
		handles := make([]int64, len(ordered))
		for i, fields := range ordered {
			var err error
			handles[i], err = symbolhandle.Pack(fields)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(sort.SliceIsSorted(handles, func(i, j int) bool { return handles[i] < handles[j] })).To(BeTrue(), "%x", handles)
	})

	DescribeTable("parse the eight symbol kinds into their 3-bit codes",
		func(name string, code symbolhandle.Kind) {
			kind, err := symbolhandle.ParseKind(name)
			Expect(err).ToNot(HaveOccurred())
			Expect(kind).To(Equal(code))
			Expect(kind.String()).To(Equal(name))
		},
		Entry(nil, "package", symbolhandle.Kind(0)),
		Entry(nil, "type", symbolhandle.Kind(1)),
		Entry(nil, "func", symbolhandle.Kind(2)),
		Entry(nil, "method", symbolhandle.Kind(3)),
		Entry(nil, "field", symbolhandle.Kind(4)),
		Entry(nil, "var", symbolhandle.Kind(5)),
		Entry(nil, "const", symbolhandle.Kind(6)),
		Entry(nil, "builtin", symbolhandle.Kind(7)),
	)

	It("rejects an unknown kind and visibility by name", func() {
		_, err := symbolhandle.ParseKind("label")
		Expect(err).To(MatchError(ContainSubstring(`kind "label"`)))
		_, err = symbolhandle.ParseVisibility("public")
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
			}, int64(0x0020_003B_0000_0000), int64(0x0020_003B_FFFF_FFFF)),
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
