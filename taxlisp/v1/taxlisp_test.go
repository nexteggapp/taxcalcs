// Package v1_test verifies that every descriptor in taxlisp/v1/taxlisp.proto
// has a leading doc comment.
package v1_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

//go:embed taxlisp.proto
var files embed.FS

func TestTaxlispProtoIsDocumented(t *testing.T) {
	b, err := files.ReadFile("taxlisp.proto")
	if err != nil {
		t.Fatal(err)
	}
	const path = "taxcalcs/taxlisp/v1/taxlisp.proto"
	c := protocompile.Compiler{
		SourceInfoMode: protocompile.SourceInfoStandard,
		Resolver: protocompile.ResolverFunc(func(p string) (protocompile.SearchResult, error) {
			if p == path {
				return protocompile.SearchResult{Source: io.NopCloser(bytes.NewReader(b))}, nil
			}
			fd, err := protoregistry.GlobalFiles.FindFileByPath(p)
			if err != nil {
				return protocompile.SearchResult{}, fmt.Errorf("%s: %w", p, err)
			}
			return protocompile.SearchResult{Desc: fd}, nil
		}),
	}
	compiled, err := c.Compile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range compiled {
		locs := fd.SourceLocations()
		check := func(d protoreflect.Descriptor) {
			if strings.TrimSpace(locs.ByDescriptor(d).LeadingComments) == "" {
				t.Errorf("%s: %s has no doc comment", fd.Path(), d.FullName())
			}
		}
		var walk func(protoreflect.MessageDescriptors)
		walk = func(ms protoreflect.MessageDescriptors) {
			for i := 0; i < ms.Len(); i++ {
				md := ms.Get(i)
				check(md)
				for j := 0; j < md.Fields().Len(); j++ {
					check(md.Fields().Get(j))
				}
				for j := 0; j < md.Oneofs().Len(); j++ {
					if !md.Oneofs().Get(j).IsSynthetic() {
						check(md.Oneofs().Get(j))
					}
				}
				walk(md.Messages())
			}
		}
		walk(fd.Messages())
	}
}
