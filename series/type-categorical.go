package series

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
)

// CategoricalSpec describes the level domain and ordering of a Categorical
// Series.
type CategoricalSpec struct {
	// Levels is the ordered list of categories. When it is non nil the
	// categories form a fixed domain: values that are not present in Levels
	// are treated as missing and are stored as NA.
	Levels []string
	// Ordered indicates that the order of Levels is meaningful. Ordered
	// categoricals are sorted according to their level order instead of
	// lexicographically.
	Ordered bool
	// Fixed indicates that the level domain is fixed. It is true by default
	// when Levels is provided.
	Fixed bool
}

// CategoricalOption configures the construction of a Categorical Series.
type CategoricalOption func(*categoricalConfig)

type categoricalConfig struct {
	levels   []string
	ordered  bool
	fixed    bool
	fixedSet bool
}

func (c *categoricalConfig) spec() CategoricalSpec {
	levels := c.levels
	if levels != nil {
		levels = append([]string{}, levels...)
	}
	fixed := c.fixed
	if levels != nil && !c.fixedSet {
		fixed = true
	}
	return CategoricalSpec{Levels: levels, Ordered: c.ordered, Fixed: fixed}
}

func stringSliceFromArg(v interface{}, more []string) []string {
	switch val := v.(type) {
	case nil:
		if len(more) == 0 {
			return nil
		}
		return append([]string{}, more...)
	case []string:
		out := append([]string{}, val...)
		return append(out, more...)
	case []interface{}:
		out := make([]string, 0, len(val)+len(more))
		for _, x := range val {
			out = append(out, fmt.Sprint(x))
		}
		return append(out, more...)
	case string:
		out := []string{val}
		return append(out, more...)
	default:
		out := []string{fmt.Sprint(v)}
		return append(out, more...)
	}
}

// WithLevels sets the level domain of a Categorical Series. By default the
// domain is fixed, which means that assigning a value outside of the declared
// levels results in a missing value. It accepts a []string, a string followed
// by the remaining levels, or an []interface{}.
func WithLevels(levels interface{}, more ...string) CategoricalOption {
	return func(c *categoricalConfig) {
		c.levels = stringSliceFromArg(levels, more)
		if !c.fixedSet {
			c.fixed = true
		}
	}
}

// WithCategories is an alias for WithLevels.
func WithCategories(categories interface{}, more ...string) CategoricalOption {
	return WithLevels(categories, more...)
}

// WithOrdered marks the categorical as ordered. When ordered is omitted it
// defaults to true.
func WithOrdered(ordered ...bool) CategoricalOption {
	b := true
	if len(ordered) > 0 {
		b = ordered[0]
	}
	return func(c *categoricalConfig) {
		c.ordered = b
	}
}

// Ordered is an alias for WithOrdered.
func Ordered(ordered ...bool) CategoricalOption {
	return WithOrdered(ordered...)
}

// WithFixed sets whether values outside of the declared levels are treated as
// missing.
func WithFixed(fixed ...bool) CategoricalOption {
	b := true
	if len(fixed) > 0 {
		fixed = fixed[:1]
		b = fixed[0]
	}
	return func(c *categoricalConfig) {
		c.fixed = b
		c.fixedSet = true
	}
}

// WithFixedDomain is an alias for WithFixed.
func WithFixedDomain(fixed ...bool) CategoricalOption {
	return WithFixed(fixed...)
}

// WithSpec applies a CategoricalSpec.
func WithSpec(spec CategoricalSpec) CategoricalOption {
	return func(c *categoricalConfig) {
		if spec.Levels != nil {
			c.levels = append([]string{}, spec.Levels...)
			c.fixed = spec.Fixed
			c.fixedSet = true
		}
		c.ordered = spec.Ordered
	}
}

// categoryDictionary is the column level dictionary shared by all of the
// elements of a Categorical Series.
type categoryDictionary struct {
	labels   []string
	index    map[string]int32
	ordered  bool
	fixed    bool
	explicit bool
}

func newCategoryDictionary(levels []string, ordered, fixed, explicit bool) *categoryDictionary {
	d := &categoryDictionary{
		index:    make(map[string]int32, len(levels)),
		ordered:  ordered,
		fixed:    fixed,
		explicit: explicit,
	}
	for _, level := range levels {
		if level == "NaN" {
			continue
		}
		if _, ok := d.index[level]; ok {
			continue
		}
		d.index[level] = int32(len(d.labels))
		d.labels = append(d.labels, level)
	}
	return d
}

func (d *categoryDictionary) clone() *categoryDictionary {
	if d == nil {
		return newCategoryDictionary(nil, false, false, false)
	}
	c := &categoryDictionary{
		labels:   append([]string{}, d.labels...),
		index:    make(map[string]int32, len(d.index)),
		ordered:  d.ordered,
		fixed:    d.fixed,
		explicit: d.explicit,
	}
	for k, v := range d.index {
		c.index[k] = v
	}
	return c
}

func (d *categoryDictionary) lookup(label string) (int32, bool) {
	if d == nil || label == "NaN" {
		return -1, false
	}
	code, ok := d.index[label]
	return code, ok
}

func (d *categoryDictionary) add(label string) int32 {
	code := int32(len(d.labels))
	d.labels = append(d.labels, label)
	if d.index == nil {
		d.index = make(map[string]int32)
	}
	d.index[label] = code
	return code
}

// categoryElement stores a single dictionary code. A code smaller than zero
// represents a missing value.
type categoryElement struct {
	code int32
	dict *categoryDictionary
}

// force categoryElement struct to implement Element interface
var _ Element = (*categoryElement)(nil)

func (e *categoryElement) Set(value interface{}) {
	e.code = -1
	switch val := value.(type) {
	case nil:
		return
	case struct{}:
		return
	case string:
		e.setLabel(val)
	case []byte:
		e.setLabel(string(val))
	case int:
		e.setLabel(strconv.Itoa(val))
	case int8:
		e.setLabel(strconv.FormatInt(int64(val), 10))
	case int16:
		e.setLabel(strconv.FormatInt(int64(val), 10))
	case int32:
		e.setLabel(strconv.FormatInt(int64(val), 10))
	case int64:
		e.setLabel(strconv.FormatInt(val, 10))
	case float32:
		e.setFloatLabel(float64(val))
	case float64:
		e.setFloatLabel(val)
	case bool:
		if val {
			e.setLabel("true")
		} else {
			e.setLabel("false")
		}
	case Element:
		if val == nil || val.IsNA() {
			return
		}
		if ce, ok := val.(*categoryElement); ok {
			if label, ok := ce.label(); ok {
				e.setLabel(label)
			}
			return
		}
		e.setLabel(val.String())
	default:
		// Named string types (for example `type Grade string`) are common
		// dimension columns, so accept any value of string kind. Anything else
		// is recorded as missing, like the other Series types do.
		if value != nil {
			rv := reflect.ValueOf(value)
			if rv.Kind() == reflect.String {
				e.setLabel(rv.String())
			}
		}
	}
}

func (e *categoryElement) setFloatLabel(val float64) {
	if math.IsNaN(val) {
		return
	}
	e.setLabel(strconv.FormatFloat(val, 'f', -1, 64))
}

func (e *categoryElement) setLabel(label string) {
	if label == "NaN" || e.dict == nil {
		return
	}
	if code, ok := e.dict.lookup(label); ok {
		e.code = code
		return
	}
	if e.dict.fixed {
		return
	}
	e.code = e.dict.add(label)
}

func (e categoryElement) label() (string, bool) {
	if e.code < 0 || e.dict == nil || int(e.code) >= len(e.dict.labels) {
		return "", false
	}
	return e.dict.labels[e.code], true
}

func (e categoryElement) Copy() Element {
	c := e
	return &c
}

func (e categoryElement) IsNA() bool {
	_, ok := e.label()
	return !ok
}

func (e categoryElement) Type() Type {
	return Categorical
}

func (e categoryElement) Val() ElementValue {
	label, ok := e.label()
	if !ok {
		return nil
	}
	return label
}

func (e categoryElement) String() string {
	label, ok := e.label()
	if !ok {
		return "NaN"
	}
	return label
}

func (e categoryElement) Int() (int, error) {
	if e.IsNA() {
		return 0, fmt.Errorf("can't convert NaN to int")
	}
	return 0, fmt.Errorf("can't convert Categorical to int")
}

func (e categoryElement) Float() float64 {
	return math.NaN()
}

func (e categoryElement) Bool() (bool, error) {
	if e.IsNA() {
		return false, fmt.Errorf("can't convert NaN to bool")
	}
	return false, fmt.Errorf("can't convert Categorical to bool")
}

func (e categoryElement) Eq(elem Element) bool {
	label, ok := e.label()
	if !ok || elem == nil || elem.IsNA() {
		return false
	}
	return label == elem.String()
}

func (e categoryElement) Neq(elem Element) bool {
	label, ok := e.label()
	if !ok || elem == nil || elem.IsNA() {
		return false
	}
	return label != elem.String()
}

// labelOf returns the label of any Element and whether it is not missing.
func labelOf(elem Element) (string, bool) {
	if elem == nil || elem.IsNA() {
		return "", false
	}
	return elem.String(), true
}

// orderedLess returns the level based comparison of two labels. The second
// return value is false when neither dictionary declares an order.
func (e categoryElement) orderedLess(other string, elem Element) (bool, bool) {
	if e.dict != nil && e.dict.ordered {
		a, okA := e.dict.lookup(e.stringLabel())
		b, okB := e.dict.lookup(other)
		if okA && okB {
			return a < b, true
		}
	}
	if oe, ok := elem.(*categoryElement); ok && oe.dict != nil && oe.dict.ordered {
		a, okA := oe.dict.lookup(e.stringLabel())
		b, okB := oe.dict.lookup(other)
		if okA && okB {
			return a < b, true
		}
	}
	return false, false
}

func (e categoryElement) stringLabel() string {
	label, _ := e.label()
	return label
}

func (e categoryElement) Less(elem Element) bool {
	label, ok := e.label()
	if !ok {
		return false
	}
	other, ok := labelOf(elem)
	if !ok {
		return false
	}
	if less, ranked := e.orderedLess(other, elem); ranked {
		return less
	}
	return label < other
}

func (e categoryElement) LessEq(elem Element) bool {
	label, ok := e.label()
	if !ok {
		return false
	}
	other, ok := labelOf(elem)
	if !ok {
		return false
	}
	if less, ranked := e.orderedLess(other, elem); ranked {
		return less || label == other
	}
	return label <= other
}

func (e categoryElement) Greater(elem Element) bool {
	label, ok := e.label()
	if !ok {
		return false
	}
	other, ok := labelOf(elem)
	if !ok {
		return false
	}
	if less, ranked := e.orderedLess(other, elem); ranked {
		return !less && label != other
	}
	return label > other
}

func (e categoryElement) GreaterEq(elem Element) bool {
	label, ok := e.label()
	if !ok {
		return false
	}
	other, ok := labelOf(elem)
	if !ok {
		return false
	}
	if less, ranked := e.orderedLess(other, elem); ranked {
		return !less
	}
	return label >= other
}

// categoryElements is the concrete implementation of Elements for Categorical
// elements. Values are stored as integer codes into a column level dictionary.
type categoryElements struct {
	elements []categoryElement
	dict     *categoryDictionary
}

func newCategoryElements(n int, dict *categoryDictionary) *categoryElements {
	if dict == nil {
		dict = newCategoryDictionary(nil, false, false, false)
	}
	elems := make([]categoryElement, n)
	for i := range elems {
		elems[i] = categoryElement{code: -1, dict: dict}
	}
	return &categoryElements{elements: elems, dict: dict}
}

func (e *categoryElements) Len() int { return len(e.elements) }

func (e *categoryElements) Elem(i int) Element { return &e.elements[i] }

func (e *categoryElements) String() string {
	labels := make([]string, len(e.elements))
	for i := range e.elements {
		labels[i] = e.elements[i].String()
	}
	return fmt.Sprint(labels)
}

// MergeLevels merges the level dictionary of x into s without adding rows.
// This is used by joins so that levels that only appear on one side, but not in
// any output row, are not lost. It returns an error if both Series are ordered
// categoricals with incompatible level orders.
func (s Series) MergeLevels(x Series) Series {
	dst, ok := s.elements.(*categoryElements)
	if !ok {
		return s
	}
	src, ok := x.elements.(*categoryElements)
	if !ok {
		return s
	}
	if src.dict == nil {
		return s
	}
	if dst.dict != nil && dst.dict.ordered && src.dict.ordered &&
		!orderedLevelsCompatible(dst.dict.labels, src.dict.labels) {
		s.Err = fmt.Errorf("categorical: incompatible level order")
		return s
	}
	merged := dst.dict.clone()
	extra, err := mergedDictionary(merged, src.dict)
	if err != nil {
		s.Err = err
		return s
	}
	// MergeLevels only widens the domain without adding rows, so both sides
	// contribute their domain properties.
	extra.fixed = merged.fixed || extra.fixed
	extra.explicit = extra.explicit || merged.explicit
	remapElements(dst.elements, dst.dict, extra)
	dst.dict = extra
	return s
}

// Codes returns the integer codes of a Categorical Series, using -1 for
// missing values. It returns nil for non categorical Series.
func (s Series) Codes() []int {
	ce, ok := s.elements.(*categoryElements)
	if !ok {
		return nil
	}
	codes := make([]int, len(ce.elements))
	for i, elem := range ce.elements {
		if _, ok := elem.label(); !ok {
			codes[i] = -1
			continue
		}
		codes[i] = int(elem.code)
	}
	return codes
}

// Categories returns the ordered category labels of a Categorical Series. It
// returns nil for non categorical Series.
func (s Series) Categories() []string {
	return s.Levels()
}

// NewCategoricalFromSeries converts a Series into a Categorical Series. Only
// string like Series can be converted.
func NewCategoricalFromSeries(s Series, args ...interface{}) Series {
	if _, ok := s.elements.(*categoryElements); ok {
		return s.Copy()
	}
	switch s.t {
	case String, Categorical:
	default:
		s.Err = fmt.Errorf("categorical: can't convert %v Series to categorical", s.t)
		return s
	}
	return NewCategorical(s, append([]interface{}{s.Name}, args...)...)
}

// CategoricalFromSeries is an alias for NewCategoricalFromSeries.
func CategoricalFromSeries(s Series, args ...interface{}) Series {
	return NewCategoricalFromSeries(s, args...)
}

// SetLevels replaces the level domain and ordering of a Categorical Series.
// Values that fall outside of a fixed domain become missing.
func (s Series) SetLevels(levels []string, ordered bool) Series {
	return s.WithCategoricalSpec(CategoricalSpec{Levels: levels, Ordered: ordered, Fixed: true})
}

// SetOrdered marks a Categorical Series as ordered or unordered.
func (s Series) SetOrdered(ordered bool) Series {
	return s.WithCategoricalSpec(CategoricalSpec{Ordered: ordered, Fixed: s.HasFixedLevels()})
}

// WithCategoricalSpec returns a Categorical Series with the given level domain
// and ordering applied.
func (s Series) WithCategoricalSpec(spec CategoricalSpec) Series {
	if _, ok := s.elements.(*categoryElements); !ok {
		s.Err = fmt.Errorf("categorical: series is not categorical")
		return s
	}
	if spec.Levels == nil && !spec.Fixed {
		// Only the ordering changes: keep the current domain.
		cur, _ := s.CategoricalSpec()
		spec.Levels = cur.Levels
	}
	return applyCategoricalSpec(s, spec)
}

func conformCategorical(src *categoryElements, dict *categoryDictionary) *categoryElements {
	if src == nil {
		return src
	}
	conformed := newCategoryElements(len(src.elements), dict.clone())
	for i, elem := range src.elements {
		label, ok := elem.label()
		if !ok {
			continue
		}
		conformed.elements[i].setLabel(label)
	}
	return conformed
}

// sortInferred sorts the levels of an inferred (non explicit) dictionary so
// that unordered categoricals have a deterministic, lexical level order.
func (e *categoryElements) sortInferred() {
	d := e.dict
	if d == nil || d.ordered || d.explicit || d.fixed || len(d.labels) < 2 {
		return
	}
	old := d.labels
	sorted := append([]string{}, old...)
	sort.Strings(sorted)
	index := make(map[string]int32, len(sorted))
	for i, label := range sorted {
		index[label] = int32(i)
	}
	for i := range e.elements {
		code := e.elements[i].code
		if code < 0 || int(code) >= len(old) {
			e.elements[i].code = -1
			continue
		}
		e.elements[i].code = index[old[code]]
	}
	d.labels = sorted
	d.index = index
}

// appendCategorical appends the elements of src to dst, merging the level
// dictionaries. Codes are remapped so that equal labels keep matching even when
// the two columns used different encodings.
func appendCategorical(dst, src *categoryElements) error {
	return appendCategoricalWithMode(dst, src, false)
}

// appendCategoricalWithMode appends src to dst. When union is false and dst
// declares a fixed domain, labels outside of that domain become missing.
func appendCategoricalWithMode(dst, src *categoryElements, union bool) error {
	if src == nil || len(src.elements) == 0 {
		return nil
	}
	if dst.dict != nil && dst.dict.ordered && src.dict != nil && src.dict.ordered &&
		!orderedLevelsCompatible(dst.dict.labels, src.dict.labels) {
		return fmt.Errorf("categorical: incompatible level order")
	}
	// Fast path: appending cannot change the destination dictionary. That is
	// always the case for a fixed destination, because labels outside of the
	// fixed domain are dropped, and otherwise it holds when the source adds no
	// level, ordering or fixed domain information. The codes are then
	// translated directly, which keeps joins and row wise transforms linear in
	// the number of rows.
	fixedDst := dst.dict != nil && dst.dict.fixed && !union
	if fixedDst || dictionaryCovers(dst.dict, src.dict) {
		appended := make([]categoryElement, len(src.elements))
		for i := range src.elements {
			code := int32(-1)
			if label, ok := src.elements[i].label(); ok {
				if c, found := dst.dict.lookup(label); found {
					code = c
				}
			}
			appended[i] = categoryElement{code: code, dict: dst.dict}
		}
		dst.elements = append(dst.elements, appended...)
		return nil
	}
	var merged *categoryDictionary
	if dst.dict != nil && dst.dict.fixed && !union {
		// The receiver declares a fixed domain: appended labels outside of it
		// are recorded as missing instead of extending the domain.
		merged = dst.dict.clone()
	} else {
		var err error
		merged, err = mergedDictionary(dst.dict, src.dict)
		if err != nil {
			return err
		}
		// The receiver decides whether the domain stays open after the merge.
		merged.fixed = dst.dict != nil && dst.dict.fixed
	}
	// When the merge does not change the destination domain, the existing
	// column keeps its codes and dictionary and only the appended elements need
	// to be mapped. This keeps building a categorical column one element at a
	// time (joins, row wise transforms) linear instead of quadratic in the
	// number of rows, and keeps every element of the column sharing a single
	// dictionary instance.
	target := merged
	if sameDictionary(dst.dict, merged) {
		target = dst.dict
	} else {
		remapElements(dst.elements, dst.dict, merged)
	}
	dst.dict = target
	appended := make([]categoryElement, len(src.elements))
	remapElementsInto(appended, src.elements, src.dict, target)
	dst.elements = append(dst.elements, appended...)
	return nil
}

// dictionaryCovers reports whether every level, and every domain property, of
// src is already present in dst. When it is, appending src cannot change the
// destination dictionary, so only the codes need to be translated.
func dictionaryCovers(dst, src *categoryDictionary) bool {
	if dst == nil {
		return false
	}
	if src == nil {
		return true
	}
	if (src.ordered && !dst.ordered) || (src.fixed && !dst.fixed) || (src.explicit && !dst.explicit) {
		return false
	}
	for _, label := range src.labels {
		if _, ok := dst.lookup(label); !ok {
			return false
		}
	}
	return true
}

// sameDictionary reports whether two dictionaries encode the same domain, so
// that elements encoded with the first one are still valid with the second.
func sameDictionary(a, b *categoryDictionary) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.ordered != b.ordered || a.fixed != b.fixed || a.explicit != b.explicit {
		return false
	}
	if len(a.labels) != len(b.labels) {
		return false
	}
	for i := range a.labels {
		if a.labels[i] != b.labels[i] {
			return false
		}
	}
	return true
}

func remapElements(elems []categoryElement, old, new *categoryDictionary) {
	for i := range elems {
		elems[i] = remapElement(elems[i], old, new)
	}
}

func remapElementsInto(dst, src []categoryElement, old, new *categoryDictionary) {
	for i := range src {
		dst[i] = remapElement(src[i], old, new)
	}
}

func remapElement(elem categoryElement, old, new *categoryDictionary) categoryElement {
	elem.dict = new
	if elem.code < 0 || old == nil || int(elem.code) >= len(old.labels) {
		elem.code = -1
		return elem
	}
	label := old.labels[elem.code]
	code, ok := new.lookup(label)
	if !ok {
		elem.code = -1
		return elem
	}
	elem.code = code
	return elem
}

func mergedDictionary(dst, src *categoryDictionary) (*categoryDictionary, error) {
	if dst == nil {
		dst = newCategoryDictionary(nil, false, false, false)
	}
	if src == nil {
		src = newCategoryDictionary(nil, false, false, false)
	}
	// A side without any declared levels or ordering contributes nothing and
	// must not alter the other side's level order.
	if len(src.labels) == 0 && !src.ordered && !src.explicit && !src.fixed {
		return dst.clone(), nil
	}
	if len(dst.labels) == 0 && !dst.ordered && !dst.explicit && !dst.fixed {
		return src.clone(), nil
	}
	if dst.ordered && src.ordered && !orderedLevelsCompatible(dst.labels, src.labels) {
		return nil, fmt.Errorf("categorical: incompatible level order")
	}
	ordered := dst.ordered || src.ordered
	explicit := dst.explicit || src.explicit || ordered
	fixed := dst.fixed || src.fixed
	var levels []string
	switch {
	case dst.ordered && src.ordered:
		levels = mergeOrderedLevels(dst.labels, src.labels)
	case dst.ordered:
		levels = appendMissingLevels(dst.labels, src.labels, false)
	case src.ordered:
		levels = appendMissingLevels(src.labels, dst.labels, false)
	case dst.explicit || src.explicit:
		levels = appendMissingLevels(dst.labels, src.labels, false)
	default:
		levels = sortedUnionLevels(dst.labels, src.labels)
	}
	return newCategoryDictionary(levels, ordered, fixed, explicit), nil
}

func orderedLevelsCompatible(a, b []string) bool {
	pos := make(map[string]int, len(b))
	for i, label := range b {
		pos[label] = i
	}
	last := -1
	for _, label := range a {
		if p, ok := pos[label]; ok {
			if p < last {
				return false
			}
			last = p
		}
	}
	return true
}

func mergeOrderedLevels(a, b []string) []string {
	result := append([]string{}, a...)
	prev := -1
	for i, label := range b {
		if pos, ok := indexOfLevel(result, label); ok {
			if pos > prev {
				prev = pos
			}
			continue
		}
		at := len(result)
		for j := i + 1; j < len(b); j++ {
			if pos, ok := indexOfLevel(result, b[j]); ok {
				at = pos
				break
			}
		}
		if at < prev+1 {
			at = prev + 1
		}
		result = append(result, "")
		copy(result[at+1:], result[at:])
		result[at] = label
		prev = at
	}
	return result
}

func appendMissingLevels(base, extra []string, _ bool) []string {
	result := append([]string{}, base...)
	seen := make(map[string]struct{}, len(result))
	for _, label := range result {
		seen[label] = struct{}{}
	}
	for _, label := range extra {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	return result
}

func sortedUnionLevels(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, label := range append(append([]string{}, a...), b...) {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	sort.Strings(result)
	return result
}

func indexOfLevel(levels []string, label string) (int, bool) {
	for i, l := range levels {
		if l == label {
			return i, true
		}
	}
	return 0, false
}

// NewCategorical creates a Categorical Series. It accepts a name and any
// combination of CategoricalOption values, a CategoricalSpec, a []string of
// levels or a bool indicating whether the levels are ordered.
func NewCategorical(values interface{}, args ...interface{}) Series {
	cfg := categoricalConfig{}
	name := ""
	for _, arg := range args {
		switch v := arg.(type) {
		case nil:
			continue
		case string:
			name = v
		case CategoricalOption:
			v(&cfg)
		case CategoricalSpec:
			if v.Levels != nil {
				cfg.levels = append([]string{}, v.Levels...)
				cfg.fixed = v.Fixed
				cfg.fixedSet = true
			}
			cfg.ordered = v.Ordered
		case *CategoricalSpec:
			if v != nil {
				if v.Levels != nil {
					cfg.levels = append([]string{}, v.Levels...)
					cfg.fixed = v.Fixed
					cfg.fixedSet = true
				}
				cfg.ordered = v.Ordered
			}
		case []string:
			WithLevels(v)(&cfg)
		case bool:
			WithOrdered(v)(&cfg)
		default:
			// Accept any func of the right shape as an option.
			if f, ok := arg.(func(*categoricalConfig)); ok {
				f(&cfg)
			}
		}
	}
	s := New(values, Categorical, name)
	return applyCategoricalSpec(s, cfg.spec())
}

// Categoricals is a constructor for a Categorical Series.
func Categoricals(values interface{}, args ...interface{}) Series {
	return NewCategorical(values, args...)
}

// NewCategoricalSeries is a convenience constructor for a Categorical Series
// with an explicit name, levels and ordering.
func NewCategoricalSeries(values interface{}, name string, levels []string, ordered bool) Series {
	if levels == nil {
		return NewCategorical(values, name, WithOrdered(ordered))
	}
	return NewCategorical(values, name, WithLevels(levels), WithOrdered(ordered))
}

func applyCategoricalSpec(s Series, spec CategoricalSpec) Series {
	ce, ok := s.elements.(*categoryElements)
	if !ok {
		s.Err = fmt.Errorf("categorical: series is not categorical")
		return s
	}
	if spec.Levels == nil && !spec.Ordered && !spec.Fixed {
		return s
	}
	var dict *categoryDictionary
	if spec.Levels != nil {
		dict = newCategoryDictionary(spec.Levels, spec.Ordered, spec.Fixed, true)
	} else {
		dict = ce.dict.clone()
		dict.ordered = spec.Ordered
		dict.fixed = spec.Fixed
	}
	elements := make([]categoryElement, len(ce.elements))
	for i, elem := range ce.elements {
		elements[i] = remapElement(elem, ce.dict, dict)
		if ce.dict != nil && elem.code >= 0 && int(elem.code) < len(ce.dict.labels) && elements[i].code < 0 && !dict.fixed {
			label := ce.dict.labels[elem.code]
			elements[i].code = dict.add(label)
		}
	}
	ce.elements = elements
	ce.dict = dict
	return s
}
