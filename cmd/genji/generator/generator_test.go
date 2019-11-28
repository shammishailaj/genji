package generator

//go:generate go test -run=TestGenerate -update

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/asdine/genji/cmd/genji/generator/testdata"
	"github.com/asdine/genji/document"
	"github.com/asdine/genji/value"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update .golden file")

func TestGenerate(t *testing.T) {
	t.Run("Golden", func(t *testing.T) {
		structs := []Struct{
			{"Basic"},
			{"basic"},
			{"CustomFieldNames"},
		}

		f, err := os.Open("testdata/structs.go")
		require.NoError(t, err)

		var buf bytes.Buffer
		err = Generate(&buf, Config{
			Sources: []io.Reader{f},
			Structs: structs,
		})
		require.NoError(t, err)

		gp := "testdata/structs.generated.golden.go"
		if *update {
			require.NoError(t, ioutil.WriteFile(gp, buf.Bytes(), 0644))
			t.Logf("%s: golden file updated", gp)
		}

		g, err := ioutil.ReadFile(gp)
		require.NoError(t, err)

		require.Equal(t, string(g), buf.String())
	})

	t.Run("Unsupported fields", func(t *testing.T) {
		tests := []struct {
			Label     string
			FieldLine string
		}{
			{"Slice", "F []string"},
			{"Maps", "F map[int]string"},
			{"Embedded", "F"},
		}

		for _, test := range tests {
			t.Run(test.Label, func(t *testing.T) {
				src := `
					package user
				
					type User struct {
						Name string
						Age int64
						` + test.FieldLine + `
					}
				`

				var buf bytes.Buffer
				err := Generate(&buf, Config{
					Sources: []io.Reader{strings.NewReader(src)},
					Structs: []Struct{{"User"}},
				})
				require.Error(t, err)
			})
		}
	})

	t.Run("Not found", func(t *testing.T) {
		src := `
			package user
		`

		var buf bytes.Buffer
		err := Generate(&buf, Config{
			Sources: []io.Reader{strings.NewReader(src)},
			Structs: []Struct{{"User"}},
		})
		require.Error(t, err)
	})

	// this test ensures the generator only generates code for
	// top level types.
	t.Run("Top level only", func(t *testing.T) {
		src := `
			package s
		
			func foo() {
				type S struct {
					X,Y,Z string
				}

				var s S
			}
		`
		var buf bytes.Buffer
		err := Generate(&buf, Config{
			Sources: []io.Reader{strings.NewReader(src)},
			Structs: []Struct{{"S"}},
		})
		require.Error(t, err)
	})

	t.Run("Header", func(t *testing.T) {
		src := `
			package s

			type S struct {
				X,Y,Z string
			}
		`

		expectedHeader := `// Code generated by genji.
// DO NOT EDIT!

package s

import (
	"errors"

	"github.com/asdine/genji/document"
)
`

		var buf bytes.Buffer
		err := Generate(&buf, Config{
			Sources: []io.Reader{strings.NewReader(src)},
			Structs: []Struct{{"S"}},
		})
		require.NoError(t, err)

		rd := bufio.NewReader(&buf)
		var res bytes.Buffer

		for i := 0; i < 10; i++ {
			l, err := rd.ReadString('\n')
			require.NoError(t, err)
			res.WriteString(l)
		}

		require.Equal(t, expectedHeader, res.String())
	})

	t.Run("Multi field with single tag", func(t *testing.T) {
		src := `
			package user

			type User struct {
				A, B string ` + "`genji:\"ab\"`" + `
			}
		`

		var buf bytes.Buffer
		err := Generate(&buf, Config{
			Sources: []io.Reader{strings.NewReader(src)},
			Structs: []Struct{{"User"}},
		})
		require.Error(t, err)
	})
}

func TestGeneratedRecords(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		r := testdata.Basic{
			A: "A", B: 10, C: 11, D: 12,
		}

		require.Implements(t, (*document.Record)(nil), &r)

		tests := []struct {
			name string
			typ  value.Type
			data []byte
		}{
			{"A", value.String, value.EncodeString(r.A)},
			{"B", value.Int, value.EncodeInt(r.B)},
			{"C", value.Int32, value.EncodeInt32(r.C)},
			{"D", value.Int32, value.EncodeInt32(r.D)},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				f, err := r.GetField(test.name)
				require.NoError(t, err)
				require.Equal(t, test.name, f.Name)
				require.Equal(t, test.typ, f.Type)
				require.Equal(t, test.data, f.Data)
			})
		}

		var i int
		err := r.Iterate(func(f document.Field) error {
			t.Run(fmt.Sprintf("Field-%d", i), func(t *testing.T) {
				require.NotEmpty(t, f)
				require.Equal(t, tests[i].name, f.Name)
				require.Equal(t, tests[i].typ, f.Type)
				require.Equal(t, tests[i].data, f.Data)
			})
			i++
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 4, i)

		var r2 testdata.Basic
		err = r2.Scan(&r)
		require.NoError(t, err)
		require.Equal(t, r, r2)
	})
}
