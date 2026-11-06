package syntax

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
)

// The syntax tree (spec 7.2) is stored the way the rest of the compiler stores
// its tables. A Module owns every node of one file in four IdVecs, one per
// category (items, statements, expressions, type expressions), and a node
// refers to another by id, never by pointer. The children of a node that are a
// list (the arguments of a call, the statements of a block, the parameters and
// attributes of a function) are a base.ListRange over one of four shared
// pools, which Module.ExprList, StmtList, Params and Attrs turn into slices.
// The few lists that are not nodes (Attr.Args) are plain slices.
//
// Id 0 is an id like any other, so an optional child cannot be "id 0": the
// node has a Has… flag next to the id, and the id means nothing when the flag
// is false.

// ItemID names an item (a function) in Module.ItemNodes.
type ItemID uint32

// StmtID names a statement in Module.Stmts.
type StmtID uint32

// ExprID names an expression in Module.Exprs.
type ExprID uint32

// TypeExprID names a type as the program writes it (`Int`, `()`) in
// Module.Types. The type checker's own types have their own ids.
type TypeExprID uint32

// Param is one parameter of a function: `x` or `x: Int`.
type Param struct {
	Name base.Symbol
	// Span runs from the first byte of the name to the last byte of the last
	// token consumed for the parameter (the type, if it has one).
	Span base.Span
	// Type is the written type. It means something only when HasType is true.
	Type    TypeExprID
	HasType bool
}

// Attr is an attribute, `#[extern("ki_print_int")]`: a name and a list of
// string arguments.
type Attr struct {
	Name base.Symbol
	// NameSpan is the span of the name. The parser does not require the `#`,
	// the `[` and the name to be next to each other, so it cannot be worked
	// out from Span.
	NameSpan base.Span
	// Args holds the value of each string argument, with the escapes already
	// resolved. It is a slice and not a ListRange because its elements are not
	// nodes (spec 7.2). It is nil when the attribute has no arguments. The
	// parser does not change it once the attribute is built, and neither
	// should anyone else.
	Args []string
	// Span runs from the `#` to the closing `]`.
	Span base.Span
	// OneLine says that no line break lies between the `#` and the `]`, so Span
	// is a piece of one line and an edit over it stays on that line. (Inside the
	// brackets and the parentheses a line break is only a space, so an attribute
	// can run over several lines.)
	OneLine bool
}

// ItemKind says what an Item is.
type ItemKind uint8

// The kinds of item.
const (
	ItemFn ItemKind = iota
	// ItemError stands for tokens that could not be read as an item. Its Span
	// is set. If the tokens began a function or a type and its name was read
	// before they stopped making sense, Name and NameSpan are set too, and
	// ErrorOf says which of the two; otherwise Name is base.NoSymbol and
	// NameSpan is the zero Span. The function or the type is known to exist
	// then, although nothing of its signature or its body is.
	ItemError

	// The kinds of Part II come after ItemError, in the order the chapters add
	// them, so that no kind of Part I changes its value. ItemTrait, ItemImpl,
	// ItemConst and ItemImport follow in theirs.

	// ItemType is a `type` declaration: a sum, a record or an alias (see Form).
	ItemType
)

// TypeForm says which of the three forms of `type` an ItemType is. The parser
// tells them apart by the first token of what follows the name: an `=` is an
// alias, and in a `{ … }` an uppercase name starts a sum and a lowercase one a
// record (see typeItem).
type TypeForm uint8

// The forms of a type declaration.
const (
	// TypeFormSum is `type Shape { Circle(radius: Float) Rect(w: Float, h: Float) }`:
	// Item.Variants is its list.
	TypeFormSum TypeForm = iota
	// TypeFormRecord is `type Point { x: Float, y: Float }`: Item.Fields.
	TypeFormRecord
	// TypeFormAlias is `type Meters = Float`: Item.Alias.
	TypeFormAlias
)

// Variant is one case of a sum type: `None`, `Some(A)` or `Circle(radius: Float)`.
// Span runs from the name to the `)` that closes its fields, or is the name when
// it has none. Fields is a range over Module.FieldPool; read it with
// Module.Fields.
type Variant struct {
	Name           base.Symbol
	Span, NameSpan base.Span
	Fields         base.ListRange
}

// Field is a field of a variant or of a record: `radius: Float`, or `Float` in
// `Some(Float)`. Span runs from the name, or from the type if there is no name,
// to the end of the type. HasName is false for a positional field: Name is
// base.NoSymbol and NameSpan the zero Span then.
type Field struct {
	Name           base.Symbol
	Span, NameSpan base.Span
	Type           TypeExprID
	HasName        bool
}

// Item is a top-level definition: a function or a type.
type Item struct {
	Kind ItemKind
	// Span covers everything the item consists of: from the `#` of its first
	// attribute (or from `pub`, or `fn`) to the end of its body (or of its
	// signature, when it has none). NameSpan is just the name.
	Span, NameSpan base.Span
	Name           base.Symbol
	Pub            bool
	// Attrs and Params are ranges over Module.AttrPool and Module.ParamPool;
	// read them with Module.Attrs and Module.Params.
	Attrs, Params base.ListRange
	// Ret is the written return type, meaningful only when HasRet is true.
	Ret    TypeExprID
	HasRet bool
	// ParamList is the parameter list, from its `(` to its `)`: the parser does
	// not require the name, the `(` and the first parameter to be next to each
	// other, so it cannot be worked out from NameSpan or from the parameters.
	// RetClause is the return clause, from the blanks (spaces and tabs) just
	// before the `->` to the end of the return type, so it holds those blanks,
	// the arrow and the type, and nothing between the `)` and the blanks, such
	// as a comment; with no blanks it starts at the arrow. It means something
	// only when HasRet is true. Both are zero for an ItemError. They are what an
	// edit that removes the parameters or the return type is made of (E0404).
	ParamList, RetClause base.Span
	// ParamListOneLine and RetClauseOneLine say that no line break lies inside
	// ParamList and RetClause, so an edit over them stays on one line (a line
	// break inside parentheses is only a space, so a parameter list can run
	// over several lines). The way Attr.OneLine does for an attribute.
	// RetClauseOneLine means something only when HasRet is true.
	ParamListOneLine, RetClauseOneLine bool
	// Body is the block, meaningful only when HasBody is true. A function
	// without a body is a declaration whose code lives elsewhere, as in
	// `#[extern("ki_print_int")] pub fn print(x: Int)`.
	Body    ExprID
	HasBody bool

	// The fields of an ItemType (Name, NameSpan, Pub and Attrs are its own too).
	// Form says which of the three it is: Variants means something for a sum,
	// Fields for a record and Alias for an alias, and the others are zero. They
	// are ranges over Module.VariantPool and Module.FieldPool (read them with
	// Module.Variants and Module.Fields); the fields of a variant are a range of
	// their own in the same FieldPool.
	Form     TypeForm
	Variants base.ListRange
	Fields   base.ListRange
	Alias    TypeExprID

	// ErrorOf is the kind of item that an ItemError that kept its name began:
	// ItemFn (the zero value) or ItemType. An ItemError that began a type keeps
	// in Variants the variants whose name the parser read before it gave up, the
	// one that the mistake is in included and with no fields, so that name
	// resolution knows they exist.
	ErrorOf ItemKind
}

// TypeExprKind says what a TypeExpr is.
type TypeExprKind uint8

// The kinds of type expression.
const (
	// TypeNamed is a type written as a name, `Int`. The parser does not know
	// which names exist; name resolution decides that.
	TypeNamed TypeExprKind = iota
	// TypeUnit is `()`.
	TypeUnit
	// TypeError stands for a type that could not be read.
	TypeError
)

// The kinds of Part II come after TypeError, in the order the sessions add them,
// so that no kind of Part I changes its value.
const (
	// TypeTuple is `(Int, Bool)`: two or more types between parentheses. Elems
	// holds them.
	TypeTuple TypeExprKind = TypeError + 1
)

// TypeExpr is a type as the program writes it. Name is set for TypeNamed only,
// and Elems, a range over Module.TypePool (read it with Module.TypeList), for
// TypeTuple only. The Span of a tuple runs from its `(` to its `)`.
type TypeExpr struct {
	Kind  TypeExprKind
	Span  base.Span
	Name  base.Symbol
	Elems base.ListRange
}

// StmtKind says what a Stmt is.
type StmtKind uint8

// The kinds of statement.
const (
	// StmtLet is `let name [: Type] = init`.
	StmtLet StmtKind = iota
	// StmtExpr is an expression written as a statement.
	StmtExpr
	// StmtError stands for tokens that could not be read as a statement.
	StmtError
)

// Stmt is a statement of a block. For StmtLet, Name and NameSpan are the bound
// name, Type and HasType the annotation, and Expr the initializer; for
// StmtExpr only Expr is set.
type Stmt struct {
	Kind           StmtKind
	Span, NameSpan base.Span
	Name           base.Symbol
	Type           TypeExprID
	HasType        bool
	Expr           ExprID
}

// BinOp is a binary operator.
type BinOp uint8

// The binary operators. Their names start with Bin because Eq, Lt, Le, Gt and
// Ge are already the names of token kinds in this package.
const (
	BinAdd BinOp = iota
	BinSub
	BinMul
	BinDiv
	BinRem
	BinEq
	BinNe
	BinLt
	BinLe
	BinGt
	BinGe
	BinAnd
	BinOr
)

// binOpText is the text of each operator, indexed by BinOp.
var binOpText = [...]string{
	BinAdd: "+", BinSub: "-", BinMul: "*", BinDiv: "/", BinRem: "%",
	BinEq: "==", BinNe: "!=", BinLt: "<", BinLe: "<=", BinGt: ">", BinGe: ">=",
	BinAnd: "&&", BinOr: "||",
}

// String returns the operator as the program writes it: "+", "==", "&&"…
// A value that is not an operator prints as "BinOp(n)".
func (op BinOp) String() string {
	if int(op) < len(binOpText) {
		return binOpText[op]
	}
	return "BinOp(" + strconv.Itoa(int(op)) + ")"
}

// UnOp is a unary operator.
type UnOp uint8

// The unary operators: arithmetic negation (`-x`) and logical negation (`!x`).
const (
	Neg UnOp = iota
	Not
)

// unOpText is the name of each unary operator, indexed by UnOp.
var unOpText = [...]string{Neg: "neg", Not: "not"}

// String returns "neg" or "not", the names `ki emit ast` prints. A value that
// is not an operator prints as "UnOp(n)".
func (op UnOp) String() string {
	if int(op) < len(unOpText) {
		return unOpText[op]
	}
	return "UnOp(" + strconv.Itoa(int(op)) + ")"
}

// ExprKind says what an Expr is, and so which of its fields mean something.
type ExprKind uint8

// The kinds of expression.
const (
	ExprInt    ExprKind = iota // Int
	ExprBool                   // Bool
	ExprUnit                   // `()`; no field
	ExprName                   // Name
	ExprCall                   // Callee, Args, OpSpan
	ExprBinary                 // Op, OpSpan, Lhs, Rhs
	ExprUnary                  // UnOp, OpSpan, Operand
	ExprIf                     // Cond, Then, Else, HasElse
	ExprBlock                  // Stmts, Tail, HasTail
	ExprError                  // an expression that could not be read; no field

	// The kinds of Part II come after ExprError, in the order the sessions add
	// them, so that no kind of Part I changes its value.

	ExprFloat // Float
)

// The kinds of chapter 11, session 4: field access, tuple index, record literal
// and tuple. They follow ExprFloat with no gap, which is why they are a block of
// their own: in the one above `iota` is already 11 at this line, and the values
// would start at 22. The first line is written `X + 1 + iota`, and the others
// repeat it, so each is one more; a first line without iota would give every
// name the same value.
const (
	ExprField      ExprKind = ExprFloat + 1 + iota // Target, Name, NameSpan: `p.x`
	ExprTupleIndex                                 // Target, Index, Name, OpSpan: `t.0`
	ExprRecord                                     // Name, NameSpan, Fields, Base, HasBase, OpSpan: `Point { x: 1.0, ..p }`
	ExprTuple                                      // Args: `(1, true)`
)

// Expr is an expression. Every kind uses a few of the fields (see ExprKind)
// and leaves the rest zero, which keeps the node flat at the price of fields
// that do not apply. Always check Kind before reading one.
//
// The Span of an Expr covers the tokens it was read from. The one exception is
// an ExprError that stands for something that is missing (an operand, an
// initializer or the block of an `if` or of its `else`): it covers nothing, so
// the parser, in the sessions that read expressions and statements, gives it an
// empty Span at the start of the token that it found instead. That can lie past
// the end of the node that holds it: in `1 + )` the binary covers bytes 0 to 3
// and its missing operand is the empty span at 4. A pass that checks that every
// child lies inside its parent must leave those out.
type Expr struct {
	Kind ExprKind
	Span base.Span

	// Literals and names.
	Int   int64
	Bool  bool
	Name  base.Symbol
	Float float64

	// Operators. OpSpan is the span of the operator itself. For a call it is the
	// argument list, from the `(` to the `)`: the `(` is the operator of a call,
	// and the span has a width even when there are no arguments. It is not the
	// end of the callee plus one, because a blank can lie between the callee and
	// the `(` (`f (x)`) and a callee in parentheses ends before the `)` of its
	// group (`(f)(x)`).
	Op                BinOp
	UnOp              UnOp
	OpSpan            base.Span
	Lhs, Rhs, Operand ExprID

	// Callee and Args (a range over Module.ExprPool; read it with ExprList).
	Callee ExprID
	Args   base.ListRange

	// The condition and the branches of an if. Else means something only when
	// HasElse is true.
	Cond, Then, Else ExprID
	HasElse          bool

	// Stmts is a range over Module.StmtPool (read it with StmtList). Tail is
	// the expression that gives the block its value, meaningful only when
	// HasTail is true.
	Stmts   base.ListRange
	Tail    ExprID
	HasTail bool

	// Target is the expression that a field access (`p.x`) or a tuple index
	// (`t.0`) reads from. NameSpan is the span of the name after the dot of an
	// ExprField, and of the type name of an ExprRecord (Name is the name in
	// both). For an ExprTupleIndex, Index is the number, saturated at
	// math.MaxUint32 when what is written does not fit (no tuple has so many
	// elements, so a saturated index is out of range in any case), Name is the
	// digits as written, interned, so that a message can say the index that the
	// program has and not the one that was kept, and OpSpan is the digits.
	Target   ExprID
	Index    uint32
	NameSpan base.Span

	// The fields of an ExprRecord: Fields is a range over Module.FieldInitPool
	// (read it with FieldInits), in the order they are written, and Base is the
	// expression after the `..` of `Point { x: 3.0, ..p }`, meaningful only when
	// HasBase is true. OpSpan is the braces and what is between them, from the
	// `{` to the `}`, so the closing brace is its last byte.
	Fields  base.ListRange
	Base    ExprID
	HasBase bool
}

// FieldInit is one field that a record literal gives: `x: 1.0`, or `x` for
// `{ x, y }`, which gives the field x the value of the variable x (punning).
// Span is the name of the field, and Value the expression that gives it: for a
// pun it is an ExprName, with the span of the name, which name resolution
// resolves like any other.
//
// Removal is what an edit that removes this field from the literal deletes: the
// field with the comma after it and the blanks after the comma, or, for a field
// with no comma after it, the field and the comma before it. A field that is the
// only one, with no comma on either side, is the field alone. RemovalOneLine says
// that no line break lies inside Removal, so an edit over it stays on one line
// (the way Attr.OneLine does for an attribute).
type FieldInit struct {
	Name           base.Symbol
	Span           base.Span
	Value          ExprID
	Pun            bool
	Removal        base.Span
	RemovalOneLine bool
}

// Module is the syntax tree of one file.
type Module struct {
	File base.FileID
	// Items lists the top-level items in source order.
	Items []ItemID

	ItemNodes base.IdVec[ItemID, Item]
	Stmts     base.IdVec[StmtID, Stmt]
	Exprs     base.IdVec[ExprID, Expr]
	Types     base.IdVec[TypeExprID, TypeExpr]

	// The pools that the ListRanges of the nodes point into.
	ExprPool    []ExprID
	StmtPool    []StmtID
	ParamPool   []Param
	AttrPool    []Attr
	VariantPool []Variant
	FieldPool   []Field
	// FieldInitPool holds the fields of the record literals, and TypePool the
	// elements of the tuple types.
	FieldInitPool []FieldInit
	TypePool      []TypeExprID
}

// ExprList returns the expressions that r selects in ExprPool. The caller must
// not change the elements. StmtList, Params and Attrs do the same for the
// other pools.
func (m *Module) ExprList(r base.ListRange) []ExprID { return base.Slice(m.ExprPool, r) }

// StmtList returns the statements that r selects in StmtPool.
func (m *Module) StmtList(r base.ListRange) []StmtID { return base.Slice(m.StmtPool, r) }

// Params returns the parameters that r selects in ParamPool.
func (m *Module) Params(r base.ListRange) []Param { return base.Slice(m.ParamPool, r) }

// Attrs returns the attributes that r selects in AttrPool.
func (m *Module) Attrs(r base.ListRange) []Attr { return base.Slice(m.AttrPool, r) }

// Variants returns the variants that r selects in VariantPool. Fields does the
// same for FieldPool.
func (m *Module) Variants(r base.ListRange) []Variant { return base.Slice(m.VariantPool, r) }

// Fields returns the fields that r selects in FieldPool.
func (m *Module) Fields(r base.ListRange) []Field { return base.Slice(m.FieldPool, r) }

// FieldInits returns the fields of a record literal that r selects in
// FieldInitPool.
func (m *Module) FieldInits(r base.ListRange) []FieldInit { return base.Slice(m.FieldInitPool, r) }

// TypeList returns the types that r selects in TypePool: the elements of a tuple
// type.
func (m *Module) TypeList(r base.ListRange) []TypeExprID { return base.Slice(m.TypePool, r) }
