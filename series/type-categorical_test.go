package series

import (
	"math"
	"reflect"
	"testing"
)

func TestCategorical_New(t *testing.T) {
	s := NewCategorical([]string{"medium", "low", "high", "medium", "NaN"})
	if s.Err != nil {
		t.Fatalf("unexpected error: %v", s.Err)
	}
	if s.Type() != Categorical {
		t.Fatalf("expected categorical type, got %v", s.Type())
	}
	if s.Len() != 5 {
		t.Fatalf("expected 5 elements, got %d", s.Len())
	}
	// Inferred levels are sorted lexicographically and the encoding is shared.
	wantLevels := []string{"high", "low", "medium"}
	if got := s.Levels(); !reflect.DeepEqual(got, wantLevels) {
		t.Fatalf("levels = %v, want %v", got, wantLevels)
	}
	wantCodes := []int{2, 1, 0, 2, -1}
	if got := s.Codes(); !reflect.DeepEqual(got, wantCodes) {
		t.Fatalf("codes = %v, want %v", got, wantCodes)
	}
	wantStrings := []string{"medium", "low", "high", "medium", "NaN"}
	if got := s.Records(); !reflect.DeepEqual(got, wantStrings) {
		t.Fatalf("records = %v, want %v", got, wantStrings)
	}
	if !s.Elem(4).IsNA() {
		t.Fatalf("expected last element to be missing")
	}
	if got, ok := s.Elem(0).Val().(string); !ok || got != "medium" {
		t.Fatalf("Val = %v, want medium", s.Elem(0).Val())
	}
}

func TestCategorical_FixedDomain(t *testing.T) {
	s := NewCategorical(
		[]string{"low", "medium", "low"},
		WithLevels("low", "medium", "high"),
		WithOrdered(true),
	)
	if got, want := s.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if !s.IsOrdered() || !s.HasFixedLevels() {
		t.Fatalf("expected ordered fixed categorical")
	}

	// Values inside the domain keep their level.
	s.Elem(0).Set("high")
	if got := s.Elem(0).String(); got != "high" {
		t.Fatalf("Set inside domain = %v, want high", got)
	}
	// Values outside a fixed domain become missing and do not extend it.
	s.Elem(1).Set("unknown")
	if !s.Elem(1).IsNA() {
		t.Fatalf("Set outside fixed domain should be missing, got %v", s.Elem(1).String())
	}
	if got, want := s.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels changed after out of domain Set: %v", got)
	}
}

func TestCategorical_OpenDomain(t *testing.T) {
	s := NewCategorical([]string{"a"}, WithLevels("a"), WithFixed(false))
	s.Elem(0).Set("b")
	if got := s.Elem(0).String(); got != "b" {
		t.Fatalf("Set on open domain = %v, want b", got)
	}
	if got, want := s.Levels(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestCategorical_OrderedSort(t *testing.T) {
	// Business order, deliberately not alphabetical.
	s := NewCategorical(
		[]string{"medium", "high", "low", "high"},
		WithLevels("low", "medium", "high"),
		WithOrdered(true),
	)
	order := s.Order(false)
	want := []int{2, 0, 1, 3}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if !s.Elem(2).Less(s.Elem(0)) {
		t.Fatalf("expected low < medium by level order")
	}
	if !s.Elem(0).Less(s.Elem(1)) {
		t.Fatalf("expected medium < high by level order")
	}
	rev := s.Order(true)
	if !reflect.DeepEqual(rev, []int{1, 3, 0, 2}) {
		t.Fatalf("reverse order = %v", rev)
	}

	// Unordered categoricals compare lexicographically.
	u := NewCategorical([]string{"medium", "high", "low"})
	if got, want := u.Order(false), []int{1, 2, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unordered order = %v, want %v", got, want)
	}
}

func TestCategorical_CrossEncodingCompare(t *testing.T) {
	a := NewCategorical([]string{"low", "high"}, WithLevels("low", "medium", "high"), WithOrdered(true))
	b := NewCategorical([]string{"high", "low"}, WithLevels("high", "low"))

	// Codes differ but labels are compared.
	if a.Codes()[0] == b.Codes()[1] {
		t.Fatalf("expected different codes for different encodings: %v %v", a.Codes(), b.Codes())
	}
	if !a.Elem(0).Eq(b.Elem(1)) {
		t.Fatalf("low should equal low across encodings")
	}
	got := a.Compare(Eq, b.Elem(0).String())
	if got.String() != "[false true]" {
		t.Fatalf("Compare(Eq, high) = %v", got.String())
	}
	got = a.Compare(In, []string{"low", "high"})
	if got.String() != "[true true]" {
		t.Fatalf("Compare(In) = %v", got.String())
	}
}

func TestCategorical_ConcatKeepsLevels(t *testing.T) {
	a := NewCategorical([]string{"low"}, WithLevels("low", "medium", "high"), WithOrdered(true))
	// A second encoding of a subset of the same order: low=0, high=1 here, but
	// high=2 in the merged domain.
	b := NewCategorical([]string{"high"}, WithLevels("low", "high"), WithOrdered(true))

	c := a.Concat(b)
	if c.Err != nil {
		t.Fatalf("unexpected error: %v", c.Err)
	}
	if got, want := c.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if got, want := c.Records(), []string{"low", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
	// "medium" was never observed but is still part of the domain.
	if c.Codes()[0] != 0 || c.Codes()[1] != 2 {
		t.Fatalf("codes = %v, want [0 2]", c.Codes())
	}

	// A conflicting order is reported explicitly.
	bad := NewCategorical([]string{"low"}, WithLevels("high", "medium", "low"), WithOrdered(true))
	badConcat := a.Concat(bad)
	if badConcat.Err == nil {
		t.Fatalf("expected incompatible level order error")
	}

	// Appending an unobserved level from an unordered side keeps it too.
	c2 := a.Concat(NewCategorical([]string{"medium"}))
	if got, want := c2.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels after open concat = %v, want %v", got, want)
	}
}

func TestCategorical_AppendConvertsPlainValues(t *testing.T) {
	s := NewCategorical([]string{"low"}, WithLevels("low", "medium", "high"))
	s.Append("medium")
	s.Append(Strings([]string{"high", "unknown"}))
	if got, want := s.Records(), []string{"low", "medium", "high", "NaN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
	if got, want := s.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestCategorical_SubsetCopyExtend(t *testing.T) {
	s := NewCategorical(
		[]string{"low", "high", "medium"},
		WithLevels("low", "medium", "high"),
		WithOrdered(true),
	)
	sub := s.Subset([]int{2, 0})
	if got, want := sub.Records(), []string{"medium", "low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subset records = %v, want %v", got, want)
	}
	if got, want := sub.Levels(), s.Levels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("subset levels = %v, want %v", got, want)
	}
	cpy := s.Copy()
	cpy.Elem(0).Set("high")
	if s.Elem(0).String() != "low" {
		t.Fatalf("Copy should not mutate the original")
	}
	// Appending missing values keeps the domain and records NA.
	ext := s.Copy()
	ext.Append(make([]struct{}, 2))
	if got, want := ext.Len(), 5; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	if !ext.Elem(3).IsNA() || !ext.Elem(4).IsNA() {
		t.Fatalf("extended elements should be missing")
	}
	if got, want := ext.Levels(), s.Levels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("extend levels = %v, want %v", got, want)
	}
}

func TestCategorical_MergeLevels(t *testing.T) {
	a := NewCategorical([]string{"low"}, WithLevels("low", "medium"))
	b := NewCategorical([]string{"high"}, WithLevels("high"))
	a = a.MergeLevels(b)
	if a.Err != nil {
		t.Fatalf("unexpected error: %v", a.Err)
	}
	if got, want := a.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if got, want := a.Records(), []string{"low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
}

func TestCategorical_NumericAccessors(t *testing.T) {
	s := NewCategorical([]string{"low", "high"})
	if !math.IsNaN(s.Elem(0).Float()) {
		t.Fatalf("Float on categorical should be NaN")
	}
	if _, err := s.Elem(0).Int(); err == nil {
		t.Fatalf("Int on categorical should return an error")
	}
	if !math.IsNaN(s.Max()) || !math.IsNaN(s.Mean()) {
		t.Fatalf("numeric aggregations on categorical should be NaN")
	}
	if got := s.MaxStr(); got != "low" {
		t.Fatalf("MaxStr = %v, want low", got)
	}
	if got := s.MinStr(); got != "high" {
		t.Fatalf("MinStr = %v, want high", got)
	}
}

func TestCategorical_Constructors(t *testing.T) {
	s := NewCategoricalSeries([]string{"b", "a"}, "col", []string{"a", "b"}, true)
	if s.Err != nil {
		t.Fatalf("unexpected error: %v", s.Err)
	}
	if s.Name != "col" {
		t.Fatalf("name = %q", s.Name)
	}
	if !s.IsOrdered() || !s.HasFixedLevels() {
		t.Fatalf("expected ordered fixed categorical")
	}
	if got, want := s.Levels(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if got := s.SetOrdered(false); got.IsOrdered() {
		t.Fatalf("SetOrdered(false) should not be ordered")
	}
	f := NewCategoricalFromSeries(Strings([]string{"x", "y"}))
	if f.Err != nil {
		t.Fatalf("unexpected error: %v", f.Err)
	}
	if got, want := f.Levels(), []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if _, ok := NewCategoricalFromSeries(Ints([]int{1})).CategoricalSpec(); ok {
		t.Fatalf("converting an Int Series should fail")
	}
}

func TestCategorical_MapAndSliceKeepDomain(t *testing.T) {
	s := NewCategorical([]string{"low", "medium", "high"}, "grade",
		WithLevels("low", "medium", "high"), WithOrdered(true))

	mapped := s.Map(func(e Element) Element {
		if e.String() == "low" {
			e.Set("high")
		}
		return e
	})
	if got, want := mapped.Records(), []string{"high", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped records = %v, want %v", got, want)
	}
	if got, want := mapped.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped levels = %v, want %v", got, want)
	}
	if !mapped.IsOrdered() {
		t.Fatalf("Map should keep the level ordering")
	}

	fresh := NewCategorical([]string{"low", "medium", "high"}, "grade",
		WithLevels("low", "medium", "high"), WithOrdered(true))
	sliced := fresh.Slice(0, 2)
	if got, want := sliced.Records(), []string{"low", "medium"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sliced records = %v, want %v", got, want)
	}
	if got, want := sliced.Levels(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sliced levels = %v, want %v", got, want)
	}
}

func TestCategorical_BadSpecError(t *testing.T) {
	s := Strings([]string{"a"}).WithCategoricalSpec(CategoricalSpec{Levels: []string{"a"}})
	if s.Err == nil {
		t.Fatalf("expected error applying categorical spec to a String Series")
	}
}
