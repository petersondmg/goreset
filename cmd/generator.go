package main

import (
	"errors"
	"go/ast"
	"io"

	"go/importer"
	"go/types"

	"go/token"

	"strings"

	"reflect"

	"github.com/dave/jennifer/jen"
	"github.com/davecgh/go-spew/spew"
)

const (
	emptyFloat  = "0.0"
	emptyInt    = "0"
	emptyString = ""
)

// generator will work on the selected structure of one file
type generator struct {
	defs       map[*ast.Ident]types.Object
	pkg        *types.Package
	fs         *token.FileSet
	files      []*ast.File
	structures []*ast.TypeSpec
	buf        *jen.File
	pkgName    string
	dirName    string
	inited     bool
	w          io.Writer
}

func newGenerator(str []*ast.TypeSpec, dirname string, files []*ast.File, fs *token.FileSet, pkgName string, w io.Writer) *generator {
	return &generator{
		structures: str,
		pkgName:    pkgName,
		w:          w,
		dirName:    dirname,
		fs:         fs,
		files:      files,
	}
}

func (g *generator) init() error {

	/**
	initializing package parsing with the go/type
	*/

	g.defs = make(map[*ast.Ident]types.Object)
	infos := &types.Info{
		Defs: g.defs,
	}

	config := types.Config{Importer: importer.Default(), FakeImportC: true}

	//var typesPkg *types.Package
	var err error
	g.pkg, err = config.Check(g.dirName, g.fs, g.files, infos)
	if err != nil {
		return err
	}

	_, err = g.w.Write([]byte("// Code generated by github.com/mrsinham/rst. DO NOT EDIT.\n"))
	if err != nil {
		return err
	}
	g.buf = jen.NewFilePathName(g.pkg.Path(), g.pkgName)
	g.inited = true
	return nil
}

func (g *generator) do() error {

	var err error
	if !g.inited {
		err = g.init()
		if err != nil {
			return err
		}
	}
	for i := range g.structures {
		err = g.doOne(g.structures[i])
		if err != nil {
			return err
		}
	}

	return g.buf.Render(g.w)
}

func (g *generator) doOne(t *ast.TypeSpec) error {
	var st *ast.StructType
	var ok bool
	if st, ok = t.Type.(*ast.StructType); !ok {
		// TODO: prevent generator to receive only valid structure
		return errors.New("type spec is not a structtype")
	}

	// write structure func header

	var magicalCode []jen.Code

	// TODO: ensure that st.Fields is not empty
	objectID := string(t.Name.Name[0])
	for i := range st.Fields.List {
		//spew.Dump(st.Fields.List[i])

		// fieds with names
		if len(st.Fields.List[i].Names) == 0 {

			// TODO here lies the inheritance by composition
			continue
		}

		var nonil bool
		// read the current tags
		if st.Fields.List[i].Tag != nil {
			bst := reflect.StructTag(strings.Trim(st.Fields.List[i].Tag.Value, "`"))
			var tc string
			if tc = bst.Get("reset"); tc == "nonil" {
				nonil = true
			}
		}

		if typ := g.defs[st.Fields.List[i].Names[0]]; typ != nil {

			var value *jen.Statement = jen.Id(objectID).Op(".").Id(st.Fields.List[i].Names[0].Name).Op("=")
			err := writeType(typ.Type(), nonil, value)
			if err != nil {
				return err
			}

			magicalCode = append(magicalCode, value)

		}

	}

	g.buf.Func().Params(jen.Id(objectID).Op("*").Id(t.Name.Name)).Id("Reset").Params().Block(
		// here generate the code
		magicalCode...,
	)

	return nil
}

func writeType(typ types.Type, nonil bool, value *jen.Statement) error {
	switch t := typ.Underlying().(type) {
	case *types.Basic:
		bi := t.Info()
		if bi&types.IsInteger != 0 {
			value.Lit(0)
		}
		if bi&types.IsString != 0 {
			value.Lit("")
		}
	case *types.Array:
		v, err := write(t)
		if err != nil {
			return err
		}

		value.Add(jen.List(v)).Block()
	case *types.Map:
		if nonil {
			v, err := write(t)
			if err != nil {
				return err
			}
			value.Make(v)
		} else {
			value.Nil()
		}
	case *types.Pointer:
		if nonil {
			// we want to know how to write the underlying object
			v, err := write(t)
			if err != nil {
				return err
			}
			// instantiate new pointer
			value.Op("&").Add(v).Block()
		} else {
			value.Nil()
		}
	case *types.Slice:
		if nonil {
			v, err := write(t)
			if err != nil {
				return err
			}
			value.Make(jen.List(v, jen.Lit(0)))
		} else {
			value.Nil()
		}
	case *types.Chan:
		if nonil {
			v, err := write(t)
			if err != nil {
				return err
			}
			value.Make(v)
		} else {
			value.Nil()
		}

	default:
		//spew.Dump(t)
		return errors.New("unsupported type")
	}
	return nil
}

func write(typ types.Type) (*jen.Statement, error) {
	switch t := typ.(type) {
	case *types.Named:
		if o := strings.LastIndex(t.String(), "."); o >= 0 {
			return jen.Qual(t.String()[:o], t.String()[o+1:]), nil
		} else {
			return jen.Lit(t.String()), nil
		}
	case *types.Map:
		key, err := write(t.Key())
		if err != nil {
			return nil, err
		}
		var val *jen.Statement
		val, err = write(t.Elem())
		if err != nil {
			return nil, err
		}
		return jen.Map(key).Add(val), nil

	case *types.Array:
		j := jen.Index(jen.Lit(int(t.Len())))
		el, err := write(t.Elem())
		if err != nil {
			return nil, err
		}
		j.Add(el)
		return j, nil
	case *types.Slice:
		j := jen.Index()
		el, err := write(t.Elem())
		if err != nil {
			return nil, err
		}
		j.Add(el)
		return j, nil
	case *types.Pointer:
		// remove the pointer star
		id := t.String()[1:]
		if o := strings.LastIndex(id, "."); o >= 0 {
			return jen.Qual(id[:o], id[o+1:]), nil
		} else {
			return jen.Lit(id), nil
		}
	case *types.Chan:
		el, err := write(t.Elem())
		if err != nil {
			return nil, err
		}
		return jen.Chan().Add(el), nil
	default:
		spew.Dump(t)
	}
	return nil, nil
}
