//go:build bindings

package bindings_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/flanksource/uir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type wireCase struct {
	Name      string          `json:"name"`
	Hierarchy string          `json:"hierarchy"`
	Value     json.RawMessage `json:"value"`
}

func TestBindings(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Generated binding conformance")
}

var _ = Describe("Java binding", Ordered, func() {
	var root, output, classpath string
	BeforeAll(func() {
		var err error
		root, err = filepath.Abs("../..")
		Expect(err).NotTo(HaveOccurred())
		output = os.Getenv("UIR_BINDINGS_OUT")
		Expect(output).NotTo(BeEmpty(), "run task bindings:java:check")
		classpath = os.Getenv("UIR_JAVA_CLASSPATH")
		Expect(classpath).NotTo(BeEmpty(), "run task bindings:java:check")
		classes, err := os.MkdirTemp(filepath.Join(output, "java"), "conformance-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(classes)).To(Succeed())
		})
		args := []string{"-cp", classpath, "-d", classes}
		args = append(args, filepath.Join(root, "internal", "bindings", "java", "Roundtrip.java"))
		args = append(args, filepath.Join(root, "bindings", "java", "examples", "Consumer.java"))
		compiled, err := exec.Command("javac", args...).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "%s", compiled)
		classpath += string(os.PathListSeparator) + classes
	})

	It("round trips every registered kind without losing fields or adding defaults", func() {
		assertRoundtrip(exec.Command("java", "-cp", classpath, "Roundtrip"))
	})

	It("rejects unknown discriminators", func() {
		command := exec.Command("java", "-cp", classpath, "Roundtrip")
		command.Stdin = bytes.NewBufferString(`[{"name":"invalid","hierarchy":"Statement","value":{"statement_type":"unknown"}}]`)
		actual, err := command.CombinedOutput()
		Expect(err).To(HaveOccurred(), "%s", actual)
		Expect(string(actual)).To(ContainSubstring("unknown"))
	})

	It("runs the documented consumer against the built SDK archive", func() {
		command := exec.Command("java", "-cp", classpath, "Consumer")
		var stderr bytes.Buffer
		command.Stderr = &stderr
		actual, err := command.Output()
		Expect(err).NotTo(HaveOccurred(), "%s", stderr.String())
		Expect(actual).To(MatchJSON(`{"node_kind":"ref","method":"Run"}`))
	})

	DescribeTable("rejects invalid wire values before model decoding", func(value string) {
		command := exec.Command("java", "-cp", classpath, "Roundtrip")
		command.Stdin = bytes.NewBufferString(`[{"name":"invalid","hierarchy":"Statement","value":` + value + `}]`)
		actual, err := command.CombinedOutput()
		Expect(err).To(HaveOccurred(), "%s", actual)
		Expect(string(actual)).To(ContainSubstring("Invalid UIR"))
	},
		Entry("missing discriminator", `{}`),
		Entry("unknown refinement", `{"statement_type":"call","statement_refinement":"unknown"}`),
		Entry("sibling refinement", `{"statement_type":"call","statement_refinement":"call:api"}`),
		Entry("wrong refinement type", `{"statement_type":"call","statement_refinement":false}`),
		Entry("unexpected field", `{"statement_type":"call","unexpected":true}`),
	)
})

var _ = Describe("TypeScript binding", func() {
	It("round trips every registered kind and populated recursive nodes through the packaged SDK", func() {
		assertRoundtrip(exec.Command("node", "typescript/roundtrip.mjs"))
	})
})

var _ = Describe("Python binding", Ordered, func() {
	var python string
	BeforeAll(func() {
		python = os.Getenv("UIR_PYTHON_BIN")
		Expect(python).NotTo(BeEmpty(), "run task bindings:python:check")
	})

	It("round trips every registered kind through the installed wheel on Python 3.10", func() {
		assertRoundtrip(exec.Command(python, "-I", "python/roundtrip.py"))
	})

	It("runs the documented consumer against the installed wheel", func() {
		command := exec.Command(python, "-I", "../../bindings/python/examples/consumer.py")
		actual, err := command.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "%s", actual)
		Expect(actual).To(MatchJSON(`{"node_kind":"ref","method":"Run"}`))
	})
})

func assertRoundtrip(command *exec.Cmd) {
	input, err := json.Marshal(registryCases())
	Expect(err).NotTo(HaveOccurred())
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	actual, err := command.Output()
	Expect(err).NotTo(HaveOccurred(), "%s", stderr.String())
	Expect(actual).To(MatchJSON(string(input)))
	var decoded []wireCase
	Expect(json.Unmarshal(actual, &decoded)).To(Succeed())
	for _, item := range decoded {
		var restored []byte
		if item.Hierarchy == "Node" {
			value, decodeErr := uir.NodeMarshaler.UnmarshalByType(item.Value)
			Expect(decodeErr).NotTo(HaveOccurred(), "%s", item.Name)
			restored, err = uir.MarshalNode(value)
		} else {
			value, decodeErr := uir.StatementMarshaler.UnmarshalByType(item.Value)
			Expect(decodeErr).NotTo(HaveOccurred(), "%s", item.Name)
			restored, err = uir.MarshalStatement(value)
		}
		Expect(err).NotTo(HaveOccurred(), "%s", item.Name)
		Expect(restored).To(MatchJSON(string(item.Value)), "%s", item.Name)
	}
}

func registryCases() []wireCase {
	var cases []wireCase
	for _, value := range uir.Nodes {
		data, err := uir.MarshalNode(value)
		Expect(err).NotTo(HaveOccurred())
		cases = append(cases, wireCase{Name: fmt.Sprintf("%T", value), Hierarchy: "Node", Value: data})
	}
	for _, value := range uir.Statements {
		data, err := uir.MarshalStatement(value)
		Expect(err).NotTo(HaveOccurred())
		cases = append(cases, wireCase{Name: fmt.Sprintf("%T", value), Hierarchy: "Statement", Value: data})
	}
	cases = append(cases, populatedCases()...)
	return cases
}

func populatedCases() []wireCase {
	metadata := uir.Metadata{Properties: map[string]any{"empty": "", "null": nil, "false": false, "list": []any{}}}
	method := uir.MethodNode{}
	method.Identifier = uir.Identifier{Module: "example", Package: "service", Method: "Run"}
	method.Metadata = metadata
	literal := uir.LiteralStmt{Value: new("")}
	literal.Metadata = metadata
	call := uir.MethodCallStmt{}
	call.Type = "call:package"
	call.Method = method
	dispatch := uir.DispatchCallStmt{Candidates: []uir.Node{method}}
	dispatch.Method = method
	dispatch.Metadata = metadata
	block := uir.BlockStmt{Children: []uir.Statement{literal, call, dispatch}}
	block.Metadata = metadata
	method.Body = &block
	inner := uir.TypedNode{}
	inner.Identifier = uir.Identifier{Type: "Inner"}
	outer := uir.TypedNode{Types: []uir.TypedNode{inner}, Methods: []uir.MethodNode{method}}
	outer.Identifier = uir.Identifier{Type: "Outer"}
	var cases []wireCase
	for _, value := range []uir.Node{method, outer} {
		data, err := uir.MarshalNode(value)
		Expect(err).NotTo(HaveOccurred())
		cases = append(cases, wireCase{Name: fmt.Sprintf("populated %T", value), Hierarchy: "Node", Value: data})
	}
	for _, value := range []uir.Statement{literal, call, dispatch, block} {
		data, err := uir.MarshalStatement(value)
		Expect(err).NotTo(HaveOccurred())
		cases = append(cases, wireCase{Name: fmt.Sprintf("populated %T", value), Hierarchy: "Statement", Value: data})
	}
	return cases
}
