package have

import (
	"fmt"
	"strings"

	gotoken "go/token"
)

type Package struct {
	path    string
	files   []*File
	objects map[string]Object
	manager *PkgManager
	tc      *TypesContext
	fset    *gotoken.FileSet
}

func NewPackage(path string, files ...*File) *Package {
	files = append(files, builtinsFile(path))
	pkg := &Package{
		path:    path,
		files:   files,
		objects: make(map[string]Object),
		tc:      NewTypesContext(),
		fset:    gotoken.NewFileSet(),
	}
	for _, f := range files {
		f.tc = pkg.tc
		f.tfile = pkg.fset.AddFile(f.name, pkg.fset.Base(), f.size)
	}
	return pkg
}

func builtinsFile(pkgName string) *File {
	code := "package " + pkgName + `
func print(s interface: pass) bool: return false
func read() string: pass`
	return &File{
		name: "_builtin.go",
		code: code,
		size: len(code),
		pkg:  pkgName,
	}
}

// Create a package using files from a PkgLocator.
func newPackageWithManager(path string, manager *PkgManager) (*Package, error) {
	files, err := manager.locator.Locate(path)
	if err != nil {
		return nil, err
	}

	fs := gotoken.NewFileSet()

	for _, f := range files {
		f.tfile = fs.AddFile(path, fs.Base(), f.size)
	}

	pkg := &Package{
		path:    path,
		files:   files,
		objects: make(map[string]Object),
		manager: manager,
		tc:      NewTypesContext(),
		fset:    fs,
	}
	for _, f := range files {
		f.tc = pkg.tc
	}
	return pkg, nil
}

func (p *Package) Get(name string) Object {
	panic("todo")
}

/*
func (o *Package) Name() string           { return o.name }
func (o *Package) ObjectType() ObjectType { return OBJECT_PACKAGE }
*/

func topoSort(stmts []*TopLevelStmt) ([]*TopLevelStmt, error) {
	// First, build a revered graph of statement dependencies.
	type node struct {
		deps, decls map[string]bool
		stmt        *TopLevelStmt
		// There can be more than one path connectings nodes, so this
		// flag saves us time.
		processed bool
	}

	graph := make(map[string][]*node, len(stmts))
	q := []*node{}
	remains := make(map[*node]bool)

	for _, stmt := range stmts {
		deps, decls := stmt.Deps(), stmt.Decls()

		entry := node{
			deps:  make(map[string]bool, len(deps)),
			decls: make(map[string]bool, len(decls)),
			stmt:  stmt,
		}

		remains[&entry] = true

		for _, dep := range deps {
			entry.deps[dep] = true
			graph[dep] = append(graph[dep], &entry)
		}

		if len(deps) == 0 {
			q = append(q, &entry)
		}

		for _, decl := range decls {
			entry.decls[decl] = true
		}
	}

	result := make([]*TopLevelStmt, 0, len(stmts))

	for len(q) > 0 {
		entry := q[0]
		q = q[1:]

		if entry.processed {
			continue
		}
		entry.processed = true
		delete(remains, entry)

		result = append(result, entry.stmt)

		for decl := range entry.decls {
			dependents, ok := graph[decl]
			if !ok {
				continue
			}

			for _, dependent := range dependents {
				delete(dependent.deps, decl)

				if len(dependent.deps) == 0 {
					q = append(q, dependent)
				}
			}
		}
	}

	if len(remains) > 0 {
		// TODO: Print the actual loop
		allDecls := map[string]bool{}
		for _, stmt := range stmts {
			decls := stmt.Decls()
			for _, decl := range decls {
				allDecls[decl] = true
			}
		}

		looped := []string{}
		missing := false
	all:
		for remain := range remains {
			for dep := range remain.deps {
				if _, ok := allDecls[dep]; ok {
					looped = append(looped, dep)
				} else {
					missing = true
					break all
				}
			}
		}

		if !missing {
			return nil, fmt.Errorf("There's a dependency loop between nodes: %#v", looped)
		} else {
			// When len(looped) == 0 we only have a unknown identifier error, but it will be reported
			// during type checking (it's easier to produce meaningful messages there).
			for remain := range remains {
				result = append(result, remain.stmt)
			}
		}
	}

	return result, nil
}

func matchUnbounds(tc *TypesContext, imports Imports, unboundTypes map[string][]DeclaredType, unboundIdents map[string][]*Ident) (errors []error) {
	for name, ts := range unboundTypes {
		var pkg *Package
		var baseName string

		if strings.Contains(name, ".") {
			pkg = ts[0].PackagePtr().pkg
			baseName = name[strings.Index(name, ".")+1:]
		} else {
			pkg = imports.Local()
			baseName = name
		}

		obj := pkg.GetObject(baseName)
		if obj == nil {
			errors = append(errors, fmt.Errorf("Unknown type %s", name))
			continue
		}

		switch decl := obj.(type) {
		case *TypeDecl:
			for _, t := range ts {
				switch typ := t.(type) {
				case *CustomType:
					typ.Decl = decl
				default:
					errors = append(errors, fmt.Errorf("Not a named type: %s", typ))
				}
			}
		case *GenericStruct:
			for _, t := range ts {
				switch typ := t.(type) {
				case *GenericType:
					obj, _, errs := decl.Instantiate(tc, typ.Params...)
					if len(errs) > 0 {
						panic(errs[0])
					}

					typ.Generic = decl
					typ.Struct = obj.(*TypeDecl).AliasedType.(*StructType)
				default:
					errors = append(errors, fmt.Errorf("Not a named type: %s", typ))
				}
			}
		}

		delete(unboundTypes, name)
	}

	for name, ids := range unboundIdents {
		// Even when an object is not found, we don't report an error yet.
		// Running type checker can change the situation - some idents can have
		// `memberName` set to true.
		object := imports.Local().GetObject(name)
		for _, id := range ids {
			id.object = object
		}
		if object != nil {
			delete(unboundIdents, name)
		}
	}
	return
}

func (o *Package) ParseAndCheck() []error {
	var errors []error
	for _, f := range o.files {
		errors = append(errors, f.Parse()...)
	}
	if len(errors) > 0 {
		return errors
	}

	importPaths := map[string]bool{}

	for _, f := range o.files {
		for _, importStmt := range f.parser.imports {
			importPaths[importStmt.path] = true
			pkg, errs := o.manager.Load(importStmt.path)
			if len(errs) > 0 {
				errors = append(errors, errs...)
				continue
			}

			importStmt.pkg = pkg
		}
		f.parser.imports[LocalPkg] = &ImportStmt{
			name: LocalPkg,
			path: "",
			pkg:  o,
		}
	}

	if len(errors) > 0 {
		return errors
	}

	for _, f := range o.files {
		for name, obj := range f.objects {
			if _, ok := o.objects[name]; ok {
				errors = append(errors, fmt.Errorf("Redeclared %s in the same package", name))
				continue
			}
			o.objects[name] = obj
		}
	}

	for _, f := range o.files {
		for _, stmt := range f.statements {
			stmt.loadDeps()
			errors = append(errors, matchUnbounds(o.tc, f.parser.imports, stmt.unboundTypes, stmt.unboundIdents)...)
		}
	}

	if len(errors) > 0 {
		return errors
	}

	allStmts := []*TopLevelStmt{}
	for _, f := range o.files {
		allStmts = append(allStmts, f.statements...)
	}

	sorted, err := topoSort(allStmts)
	if err != nil {
		return []error{err}
	}

	for _, f := range sorted {
		typedStmt := f.Stmt.(ExprToProcess)
		if err := typedStmt.NegotiateTypes(o.tc); err != nil {
			return []error{err}
		}
	}

	if len(errors) > 0 {
		return errors
	}

	return errors
}

func (o *Package) GetObject(name string) Object {
	return o.objects[name]
}

func (o *Package) GetType(name string) *TypeDecl {
	obj, ok := o.objects[name]
	if !ok || obj.ObjectType() != OBJECT_TYPE {
		return nil
	}
	return obj.(*TypeDecl)
}

// Provides access to packages, makes sure that no package is loaded more than once, etc.
type PkgManager struct {
	pkgs map[string]*Package
	// Packages being loaded are put in this map, it's used to detect cycles.
	greyNodes map[string]bool
	// Ordered version of greyNodes, used to report errors.
	greyStack []string
	locator   PkgLocator
}

func NewPkgManager(locator PkgLocator) *PkgManager {
	return &PkgManager{
		pkgs:      make(map[string]*Package),
		greyNodes: make(map[string]bool),
		locator:   locator,
	}
}

func (m *PkgManager) Load(path string) (*Package, []error) {
	if cycle := m.greyNodes[path]; cycle {
		return nil, []error{fmt.Errorf("Import cycle: %s", strings.Join(append(m.greyStack, path), ", "))}
	}

	if pkg, ok := m.pkgs[path]; ok {
		return pkg, nil
	}

	m.greyNodes[path] = true
	m.greyStack = append(m.greyStack, path)
	defer func() {
		delete(m.greyNodes, path)
		m.greyStack = m.greyStack[:len(m.greyStack)-1]
	}()

	pkg, err := newPackageWithManager(path, m)
	if err != nil {
		return nil, []error{err}
	}
	errs := pkg.ParseAndCheck()
	if len(errs) > 0 {
		return nil, errs
	}
	m.pkgs[path] = pkg
	return pkg, nil
}

type Instantiation struct {
	FullName string
	Params   []Type
	Generic  Generic
	Object   Object
	Init     Expr // Nil for generics generating types

	goName string // cache

	parser *Parser
	tc     *TypesContext
}

func (r *Instantiation) getGoName() string {
	if r.goName == "" {
		r.goName = string(NewInstKey(r.Generic, r.Params))
		r.goName = strings.Replace(r.goName, "[", "_", -1)
		r.goName = strings.Replace(r.goName, "]", "_", -1)
		r.goName = strings.Replace(r.goName, "*", "PTR_", -1)
		r.goName = strings.TrimRight(r.goName, "_")
	}
	return r.goName
}

func (r *Instantiation) ParseAndCheck() []error {
	tfile, offset := r.Generic.Location()
	r.parser = NewParser(NewLexer(r.Generic.Code(), tfile, offset))
	// Parser sees the instantiation as a separate file, so we need to plug in imports from
	// the original file.
	r.parser.imports = r.Generic.Imports()

	// Fill parser.genericParams so that the parser can immediately substitute them with
	// concrete types.
	_, paramsList := r.Generic.Signature()
	genericParams := make(map[string]Type, len(paramsList))
	for i := 0; i < len(paramsList); i++ {
		name, val := paramsList[i], r.Params[i]
		genericParams[name] = val
	}
	//r.ParseAndCheck()
	r.parser.genericParams = genericParams
	r.parser.generic = r.Generic

	stmts, err := r.parser.Parse()
	if err != nil {
		return []error{err}
	}
	if len(stmts) != 1 {
		panic(fmt.Sprintf("Internal error: parsing a generic instantiation returned %d statements", len(stmts)))
	}

	tlStmt := stmts[0]

	errors := matchUnbounds(r.tc, r.parser.imports, tlStmt.unboundTypes, tlStmt.unboundIdents)
	if len(errors) > 0 {
		return errors
	}

	// TODO: Refactor this ugliness
	switch s := tlStmt.Stmt.(type) {
	case *VarStmt:
		// Generic func
		r.Object = s.Vars[0].Vars[0]
		r.Init = s.Vars[0].Inits[0]
		r.Init.(*FuncDecl).name = r.getGoName()
	case *StructStmt:
		r.Object = s.Decl
		s.Decl.name = r.getGoName()
		s.Decl.AliasedType.(*StructType).Name = r.getGoName()
		s.Decl.AliasedType.(*StructType).selfType.Name = r.getGoName()
	default:
		panic("Internal error")
	}

	err = tlStmt.Stmt.(ExprToProcess).NegotiateTypes(r.tc)
	if err != nil {
		return []error{err}
	}
	return nil
}
