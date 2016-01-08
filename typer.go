// Negotiate and validate types in an AST.
package have

import "fmt"

type ExprToProcess interface {
	Expr
	NegotiateTypes() error
}

type TypedExpr interface {
	//ExprToProcess
	Expr

	//TypeFullyKnown() bool
	Type() Type
	//CanBeOfType(typ Type) bool
	ApplyType(typ Type) error
	GuessType() (ok bool, typ Type)
}

func unNilType(t *Type) Type {
	if *t == nil {
		*t = &UnknownType{}
	}
	return *t
}

func nonilTyp(t Type) Type {
	if t == nil {
		return &UnknownType{}
	}
	return t
}

func (vs *VarStmt) NegotiateTypes() error {
	for _, v := range vs.Vars {
		err := v.NegotiateTypes()
		if err != nil {
			return err
		}
	}
	return nil
}

func (vd *VarDecl) NegotiateTypes() error {
	typedInit := vd.Init.(TypedExpr)

	vd.Type = nonilTyp(vd.Type)

	fmt.Printf("ZZZ MOM TYPY %s oraz %s\n", vd.Type, typedInit.Type())

	typ, err := NegotiateTypes(vd.Type, typedInit.Type())
	if err != nil || !typ.Known() {
		// Try guessing. Literals like "1", or "{1, 2}" can be used
		// to initialize variables of many types (int/double/etc,
		// array/slice/struct), but if the type of the variable is
		// unknown, we try to guess the type (for these examples
		// it would be "int" and "[]int").
		ok, guessedType := typedInit.GuessType()
		fmt.Printf("ZZZ ZGODUJA %s %s\n", ok, guessedType)
		if ok {
			typ, err = NegotiateTypes(vd.Type, guessedType)
		}

		if err != nil {
			return err
		}
	}

	vd.Type = typ
	return typedInit.ApplyType(typ)
	//if !typedInit.CanBeOfType(typ) {
	//	return fmt.Errorf("Couldn't infer type of %s", vd.Name)
	//}

	//// TODO: we've got the type, now apply it

	//return nil
}

func (ex *BlankExpr) Type() Type                     { panic("nope") }
func (ex *BlankExpr) ApplyType(typ Type) error       { panic("nope") }
func (ex *BlankExpr) GuessType() (ok bool, typ Type) { panic("nope") }

func (ex *FuncCallExpr) Type() Type {
	/*callee := ex.Left
	switch t := callee.(type) {
			*TypeExpr:
	}*/
	panic("nope")
}
func (ex *FuncCallExpr) ApplyType(typ Type) error       { panic("nope") }
func (ex *FuncCallExpr) GuessType() (ok bool, typ Type) { panic("nope") }

func (ex *TypeExpr) Type() Type { return ex.typ }
func (ex *TypeExpr) ApplyType(typ Type) error {
	if ex.typ.String() != typ.String() {
		return fmt.Errorf("Different types, %s and %s", ex.typ.String(), typ.String())
	}
	return nil
}
func (ex *TypeExpr) GuessType() (ok bool, typ Type) { return false, nil }

func (ex *DotSelector) Type() Type {
	leftType := ex.Left.(TypedExpr).Type()
	switch leftType.Kind() {
	case KIND_STRUCT:
		asStruct := leftType.(*StructType)
		member, ok := asStruct.Members[ex.Right.name]
		if !ok {
			// no such member
			return &UnknownType{}
		}
		return member
	case KIND_POINTER:
		panic("todo")
	case KIND_UNKNOWN:
		panic("todo")
	case KIND_CUSTOM:
		panic("todo")
	default:
		return &UnknownType{}
	}
}

func (ex *DotSelector) ApplyType(typ Type) error {
	if ex.Type().String() != typ.String() {
		return fmt.Errorf("Type %s has no member named %s", ex.Left.(TypedExpr).Type().String(), ex.Right.name)
	}
	return nil
}

func (ex *DotSelector) GuessType() (ok bool, typ Type) {
	return false, nil
}

func (ex *CompoundLit) Type() Type { return nonilTyp(ex.typ) }
func (ex *CompoundLit) ApplyType(typ Type) error {
	var apply = false

	switch typ.Kind() {
	case KIND_SLICE:
		asSlice := typ.(*SliceType)

		switch ex.kind {
		case COMPOUND_EMPTY:
			apply = true
		case COMPOUND_LISTLIKE:
			for _, el := range ex.elems {
				if err := el.(TypedExpr).ApplyType(asSlice.Of); err != nil {
					return err
				}
			}
			apply = true
		}
	case KIND_ARRAY:
		asArray := typ.(*ArrayType)

		switch ex.kind {
		case COMPOUND_EMPTY:
			apply = asArray.Size == 0
		case COMPOUND_LISTLIKE:
			if len(ex.elems) == asArray.Size {
				for _, el := range ex.elems {
					if err := el.(TypedExpr).ApplyType(asArray.Of); err != nil {
						return err
					}
				}
				apply = true
			}
		}
	case KIND_STRUCT:
		panic("todo")
	case KIND_MAP:
		asMap := typ.(*MapType)

		switch ex.kind {
		case COMPOUND_EMPTY:
			apply = true
		case COMPOUND_MAPLIKE:
			for i, el := range ex.elems {
				if i%2 == 0 {
					if err := el.(TypedExpr).ApplyType(asMap.By); err != nil {
						return err
					}
				} else {
					if err := el.(TypedExpr).ApplyType(asMap.Of); err != nil {
						return err
					}
				}
			}
			apply = true
		}
	}

	if apply {
		ex.typ = typ
		return nil
	}
	return fmt.Errorf("Can't use a compound literal to initialize type %s", typ.String())
}

func (ex *CompoundLit) GuessType() (ok bool, typ Type) {
	switch ex.kind {
	case COMPOUND_EMPTY:
		return false, nil
	case COMPOUND_LISTLIKE:
		var typ Type = nil
		for _, el := range ex.elems {
			ok, t := el.(TypedExpr).GuessType()
			if !ok {
				//return fmt.Errorf("Can't guess the type of the compound literal, because can't guess the type of %#v", el)
				return false, nil
			}
			if typ == nil {
				typ = nonilTyp(t)
			}
			if typ.String() != t.String() {
				return false, nil
			}
		}
		return true, &SliceType{Of: typ}
	case COMPOUND_MAPLIKE:
		var keyType, valueType Type = nil, nil
		for i, el := range ex.elems {
			ok, t := el.(TypedExpr).GuessType()
			if !ok {
				return false, nil
			}

			if i%2 == 0 {
				if keyType == nil {
					keyType = nonilTyp(t)
				}
				if keyType.String() != t.String() {
					return false, nil
				}
			} else {
				if valueType == nil {
					valueType = nonilTyp(t)
				}
				if valueType.String() != t.String() {
					return false, nil
				}
			}
		}
		return true, &MapType{By: keyType, Of: valueType}
	}
	return false, nil
}

func (ex *BinaryOp) Type() Type {
	// for now, assume Left and Right have the same types
	return ex.Left.(TypedExpr).Type()
}

func (ex *BinaryOp) ApplyType(typ Type) error {
	// TODO: Validate concrete operators and types (logical operators only for bools,
	// numeric operators for numeric types, no tuple types, etc).

	if err := ex.Left.(TypedExpr).ApplyType(typ); err != nil {
		return err
	}
	return ex.Right.(TypedExpr).ApplyType(typ)
}

func (ex *BinaryOp) GuessType() (ok bool, typ Type) {
	leftOk, leftType := ex.Left.(TypedExpr).GuessType()
	rightOk, rightType := ex.Right.(TypedExpr).GuessType()

	switch {
	case leftOk && rightOk && leftType.String() == rightType.String():
		// The clearest situation - both expressions were able to guess their types
		// and they are the same.
		return true, leftType
	case leftOk:
		err := ex.Right.(TypedExpr).ApplyType(leftType)
		if err == nil {
			return true, leftType
		}
		fallthrough
	case rightOk:
		err := ex.Left.(TypedExpr).ApplyType(rightType)
		if err == nil {
			return true, rightType
		}
		fallthrough
	default:
		return false, nil
	}
}

func (ex *UnaryOp) Type() Type {
	switch right := ex.Right.(TypedExpr); ex.op.Type {
	case TOKEN_PLUS, TOKEN_MINUS, TOKEN_SHR, TOKEN_SHL:
		return right.Type()
	case TOKEN_MUL:
		subType := right.Type()
		if subType.Kind() != KIND_POINTER {
			// underlying type is not a pointer
			return &UnknownType{}
		}
		return subType.(*PointerType).To
	case TOKEN_AMP:
		return &PointerType{To: right.Type()}
	default:
		panic("todo")
	}
}

func (ex *UnaryOp) ApplyType(typ Type) error {
	// TODO: Validate concrete operators and types (logical operators only for bools,
	// numeric operators for numeric types, no tuple types, etc).
	// The way it should be implemented is to reuse as much as possible with BinaryOp.

	switch right := ex.Right.(TypedExpr); ex.op.Type {
	case TOKEN_PLUS, TOKEN_MINUS, TOKEN_SHR, TOKEN_SHL:
		return right.ApplyType(typ)
	case TOKEN_MUL:
		return right.ApplyType(&PointerType{To: typ})
	case TOKEN_AMP:
		if typ.Kind() != KIND_POINTER {
			return fmt.Errorf("Not a pointer type")
		}
		to := typ.(*PointerType).To
		return right.ApplyType(to)
	default:
		panic("todo")
	}
}

func (ex *UnaryOp) GuessType() (ok bool, typ Type) {
	switch right := ex.Right.(TypedExpr); ex.op.Type {
	case TOKEN_PLUS, TOKEN_MINUS, TOKEN_SHR, TOKEN_SHL:
		//return right.ApplyType(typ)
		return right.GuessType()
	case TOKEN_MUL:
		ok, typ := right.GuessType()
		if !ok {
			return false, nil
		}
		if typ.Kind() != KIND_POINTER {
			return false, nil
		}
		return true, typ.(*PointerType).To
	case TOKEN_AMP:
		ok, typ := right.GuessType()
		if !ok {
			return false, nil
		}
		return true, &PointerType{To: typ}
	default:
		panic("todo")
	}
	//return ex.Right.(TypedExpr).GuessType()
}

func (ex *Ident) Type() Type {
	if ex.object != nil && ex.object.ObjectType() == OBJECT_VAR {
		return ex.object.(*VarDecl).Type
	}
	//fmt.Printf("ZZZ ret nil\n")
	return nil
}

func (ex *Ident) ApplyType(typ Type) error {
	if ex.object.ObjectType() != OBJECT_VAR {
		return fmt.Errorf("Identifier %s is not a variable", ex.name)
	}

	if ex.object.(*VarDecl).Type.String() != typ.String() {
		return fmt.Errorf("Identifier %s is of type %s", ex.name, ex.object.(*VarDecl).Type.String())
	}
	return nil
}

func (ex *Ident) GuessType() (ok bool, typ Type) {
	return false, nil
}

func (ex *BasicLit) Type() Type {
	return nonilTyp(ex.typ)
}

func (ex *BasicLit) ApplyType(typ Type) error {
	actualType := typ
	if typ.Kind() == KIND_CUSTOM {
		actualType = typ.(*CustomType).RootType()
	}

	switch {
	case ex.token.Type == TOKEN_STR &&
		actualType.Kind() == KIND_SIMPLE &&
		actualType.(*SimpleType).ID == SIMPLE_TYPE_STRING:
		fallthrough
	case ex.token.Type == TOKEN_NUM &&
		actualType.Kind() == KIND_SIMPLE &&
		actualType.(*SimpleType).ID == SIMPLE_TYPE_INT:
		fallthrough
	case (ex.token.Type == TOKEN_TRUE || ex.token.Type == TOKEN_FALSE) &&
		actualType.Kind() == KIND_SIMPLE &&
		actualType.(*SimpleType).ID == SIMPLE_TYPE_BOOL:

		ex.typ = typ
		return nil
	}
	return fmt.Errorf("Can't use this literal for this type")
}

func (ex *BasicLit) GuessType() (ok bool, typ Type) {
	switch ex.token.Type {
	case TOKEN_STR:
		return true, &SimpleType{ID: SIMPLE_TYPE_STRING}
	case TOKEN_NUM:
		// TODO: handle anything else than just integers
		return true, &SimpleType{ID: SIMPLE_TYPE_INT}
	case TOKEN_TRUE, TOKEN_FALSE:
		return true, &SimpleType{ID: SIMPLE_TYPE_BOOL}
	}
	return false, nil
}

func (t *SimpleType) Negotiate(other Type) (Type, error) {
	if other, ok := other.(*SimpleType); !ok {
		return nil, fmt.Errorf("Not a simple type")
	} else if t.ID == other.ID {
		return t, nil
	} else {
		return nil, fmt.Errorf("Different simple types, %s and %s",
			simpleTypeAsStr[t.ID], simpleTypeAsStr[other.ID])
	}
}

func (t *ArrayType) Negotiate(other Type) (Type, error) {
	if other, ok := other.(*ArrayType); !ok {
		return nil, fmt.Errorf("Not an array type")
	} else if t.Size == other.Size {
		typ, err := NegotiateTypes(t.Of, other.Of)
		if err != nil {
			return nil, err
		}
		return &ArrayType{Size: t.Size, Of: typ}, nil
	} else {
		return nil, fmt.Errorf("Different array sizes, %d and %d", t.Size, other.Size)
	}
}

func (t *SliceType) Negotiate(other Type) (Type, error) {
	if other, ok := other.(*SliceType); !ok {
		return nil, fmt.Errorf("Not a slice type")
	} else {
		typ, err := NegotiateTypes(t.Of, other.Of)
		if err != nil {
			return nil, err
		}
		return &SliceType{Of: typ}, nil
	}
}

func (t *CustomType) Negotiate(other Type) (Type, error) {
	panic("todo")
}

func (t *UnknownType) Negotiate(other Type) (Type, error) {
	return other, nil
}

func (t *PointerType) Negotiate(other Type) (Type, error) {
	panic("todo")
}

func (t *MapType) Negotiate(other Type) (Type, error) {
	panic("todo")
}

func (t *StructType) Negotiate(other Type) (Type, error) {
	panic("todo")
}

func NegotiateTypes(t1, t2 Type) (Type, error) {
	if t1.Known() && t2.Known() {
		// Both types are fully known, no negotiation needed, just
		// check if they are the same.
		if t1.String() != t2.String() {
			return nil, fmt.Errorf("Wanted type %s, but got type %s",
				t1.String(), t2.String())
		}
		return t1, nil
	}

	if t1.Kind() == t2.Kind() {
		if t1.Kind() == KIND_UNKNOWN { // && t2.Kind() == KIND_UNKNOWN
			return nil, fmt.Errorf("Too little information to infer data types")
		}
		// TODO: Maybe Negotiate() for types won't be needed at all?
		//return t1.Negotiate(t2)
		return nil, fmt.Errorf("Too little information to infer data types")
	} else if t1.Kind() == KIND_UNKNOWN {
		if !t2.Known() {
			// If one type is completely unknown, the other has to be fully known.
			return nil, fmt.Errorf("Couldn't infer type, too little information")
		}
		return t2, nil
	} else if t2.Kind() == KIND_UNKNOWN {
		if !t1.Known() {
			// If one type is completely unknown, the other has to be fully known.
			return nil, fmt.Errorf("Couldn't infer type, too little information")
		}
		return t1, nil
	}
	// TODO: use stringer
	return nil, fmt.Errorf("Different type kinds: %#v and %#v", t1.Kind(), t2.Kind())
}