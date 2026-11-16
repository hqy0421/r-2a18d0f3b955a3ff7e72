package dataframe

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/go-gota/gota/series"
)

func categoricalLevels(t *testing.T, df DataFrame, col string) []string {
	t.Helper()
	levels := df.Col(col).Levels()
	if levels == nil {
		t.Fatalf("column %q is not categorical", col)
	}
	return levels
}

func TestDataFrame_CategoricalColumns(t *testing.T) {
	df := New(
		series.NewCategorical([]string{"medium", "low", "high", "medium"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
		series.New([]int{1, 2, 3, 4}, series.Int, "v"),
	)
	if got, want := df.Types()[0], series.Categorical; got != want {
		t.Fatalf("type = %v, want %v", got, want)
	}
	if got, want := categoricalLevels(t, df, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}

	// Filtering by label.
	filtered := df.Filter(F{Colname: "grade", Comparator: series.Eq, Comparando: "medium"})
	if got, want := filtered.Col("v").Records(), []string{"1", "4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter records = %v, want %v", got, want)
	}
	if got, want := categoricalLevels(t, filtered, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered levels = %v, want %v", got, want)
	}

	// Ordered filtering by level rank, not alphabetically.
	greater := df.Filter(F{Colname: "grade", Comparator: series.Greater, Comparando: "low"})
	if got, want := greater.Col("v").Records(), []string{"1", "3", "4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered filter records = %v, want %v", got, want)
	}

	// Sorting by level rank.
	sorted := df.Arrange(Sort("grade"))
	if got, want := sorted.Col("grade").Records(), []string{"low", "medium", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted records = %v, want %v", got, want)
	}
	if got, want := sorted.Col("v").Records(), []string{"2", "1", "4", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted values = %v, want %v", got, want)
	}

	// Assigning outside of the fixed domain records a missing value.
	df2 := df.Set(
		series.Ints([]int{1}),
		New(
			series.NewCategorical([]string{"unknown"}, "grade"),
			series.New([]int{9}, series.Int, "v"),
		),
	)
	if df2.Err != nil {
		t.Fatalf("unexpected error: %v", df2.Err)
	}
	if got := df2.Col("grade").Elem(1).String(); got != "NaN" {
		t.Fatalf("out of domain assignment = %v, want NaN", got)
	}
	if got, want := categoricalLevels(t, df2, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestDataFrame_GroupByCategoricalKeepsDomain(t *testing.T) {
	df := New(
		series.NewCategorical([]string{"medium", "low", "high", "medium"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
		series.New([]int{1, 2, 3, 4}, series.Int, "v"),
	)

	groups := df.GroupBy("grade")
	if groups.Err != nil {
		t.Fatalf("unexpected GroupBy error: %v", groups.Err)
	}
	agg := groups.Aggregation([]AggregationType{Aggregation_SUM}, []string{"v"})
	if agg.Err != nil {
		t.Fatalf("unexpected Aggregation error: %v", agg.Err)
	}
	if got, want := agg.Types()[0], series.Categorical; got != want {
		t.Fatalf("group key type = %v, want %v", got, want)
	}
	// The full domain survives the GroupBy / Aggregation round trip.
	if got, want := categoricalLevels(t, agg, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregated levels = %v, want %v", got, want)
	}
	if got, want := agg.Col("grade").Records(), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("group keys = %v, want %v", got, want)
	}
	if got, want := agg.Col("v_SUM").Records(), []string{"2.000000", "5.000000", "3.000000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregated values = %v, want %v", got, want)
	}
}

func TestDataFrame_JoinCategoricalCrossEncoding(t *testing.T) {
	// The two sides encode the same labels differently.
	left := New(
		series.NewCategorical([]string{"low", "high"}, "grade",
			series.WithLevels("low", "medium", "high")),
		series.New([]int{1, 2}, series.Int, "l"),
	)
	right := New(
		series.NewCategorical([]string{"high", "low"}, "grade",
			series.WithLevels("high", "low")),
		series.New([]int{10, 20}, series.Int, "r"),
	)

	if got, want := left.Col("grade").Codes(), []int{0, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("left codes = %v, want %v", got, want)
	}
	if got, want := right.Col("grade").Codes(), []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("right codes = %v, want %v", got, want)
	}

	inner := left.InnerJoin(right, "grade")
	if inner.Err != nil {
		t.Fatalf("unexpected join error: %v", inner.Err)
	}
	if got, want := inner.Nrow(), 2; got != want {
		t.Fatalf("inner rows = %d, want %d", got, want)
	}
	records := inner.Records()
	if !reflect.DeepEqual(records[1], []string{"low", "1", "20"}) ||
		!reflect.DeepEqual(records[2], []string{"high", "2", "10"}) {
		t.Fatalf("inner records = %v", records)
	}
	// "medium" only exists in the declared domain and appears in no row, but
	// must not be lost.
	if got, want := categoricalLevels(t, inner, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inner levels = %v, want %v", got, want)
	}

	outer := left.OuterJoin(right, "grade")
	if outer.Err != nil {
		t.Fatalf("unexpected join error: %v", outer.Err)
	}
	if got, want := outer.Nrow(), 2; got != want {
		t.Fatalf("outer rows = %d, want %d", got, want)
	}
	if got, want := categoricalLevels(t, outer, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("outer levels = %v, want %v", got, want)
	}
}

func TestDataFrame_JoinCategoricalOnlyInOneSide(t *testing.T) {
	left := New(
		series.NewCategorical([]string{"low", "high"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
		series.New([]int{1, 2}, series.Int, "l"),
	)
	right := New(
		series.NewCategorical([]string{"low"}, "grade", series.WithLevels("low")),
		series.New([]int{10}, series.Int, "r"),
	)

	joined := left.LeftJoin(right, "grade")
	if joined.Err != nil {
		t.Fatalf("unexpected join error: %v", joined.Err)
	}
	if got, want := joined.Nrow(), 2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got := joined.Col("r").Elem(1).String(); got != "NaN" {
		t.Fatalf("missing right value = %v, want NaN", got)
	}
	if got, want := categoricalLevels(t, joined, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestDataFrame_RBindConcatCategorical(t *testing.T) {
	a := New(
		series.NewCategorical([]string{"low"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
		series.New([]int{1}, series.Int, "v"),
	)
	b := New(
		series.NewCategorical([]string{"high"}, "grade",
			series.WithLevels("low", "high"), series.WithOrdered(true)),
		series.New([]int{2}, series.Int, "v"),
	)

	rbound := a.RBind(b)
	if rbound.Err != nil {
		t.Fatalf("unexpected RBind error: %v", rbound.Err)
	}
	if got, want := rbound.Col("grade").Records(), []string{"low", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rbind records = %v, want %v", got, want)
	}
	if got, want := categoricalLevels(t, rbound, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rbind levels = %v, want %v", got, want)
	}

	// Concat with a column only present on one side.
	c := New(
		series.NewCategorical([]string{"medium"}, "grade",
			series.WithLevels("medium", "high")),
		series.New([]int{3}, series.Int, "w"),
	)
	concatenated := a.Concat(c)
	if concatenated.Err != nil {
		t.Fatalf("unexpected Concat error: %v", concatenated.Err)
	}
	if got, want := concatenated.Names(), []string{"grade", "v", "w"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("concat names = %v, want %v", got, want)
	}
	if got, want := categoricalLevels(t, concatenated, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("concat levels = %v, want %v", got, want)
	}
	if got, want := concatenated.Col("v").Records(), []string{"1", "NaN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("concat v = %v, want %v", got, want)
	}

	// Incompatible orderings are an explicit error.
	bad := New(
		series.NewCategorical([]string{"low"}, "grade",
			series.WithLevels("high", "medium", "low"), series.WithOrdered(true)),
		series.New([]int{9}, series.Int, "v"),
	)
	if err := a.RBind(bad).Err; err == nil {
		t.Fatalf("expected incompatible level order error from RBind")
	}
}

func TestDataFrame_CategoricalExport(t *testing.T) {
	df := New(
		series.NewCategorical([]string{"low", "high"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
		series.New([]int{1, 2}, series.Int, "v"),
	)

	csv := new(bytes.Buffer)
	if err := df.WriteCSV(csv); err != nil {
		t.Fatalf("unexpected CSV error: %v", err)
	}
	if got, want := csv.String(), "grade,v\nlow,1\nhigh,2\n"; got != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}

	json := new(bytes.Buffer)
	if err := df.WriteJSON(json); err != nil {
		t.Fatalf("unexpected JSON error: %v", err)
	}
	if got, want := json.String(), "[{\"grade\":\"low\",\"v\":1},{\"grade\":\"high\",\"v\":2}]\n"; got != want {
		t.Fatalf("json = %q, want %q", got, want)
	}
}

func TestDataFrame_CategoricalLoadRecords(t *testing.T) {
	records := [][]string{
		{"grade", "v"},
		{"low", "1"},
		{"medium", "2"},
		{"bogus", "3"},
	}
	df := LoadRecords(records, WithCategoricals(map[string]series.CategoricalSpec{
		"grade": {Levels: []string{"low", "medium", "high"}, Ordered: true, Fixed: true},
	}))
	if df.Err != nil {
		t.Fatalf("unexpected error: %v", df.Err)
	}
	if got, want := df.Types()[0], series.Categorical; got != want {
		t.Fatalf("type = %v, want %v", got, want)
	}
	if got, want := categoricalLevels(t, df, "grade"), []string{"low", "medium", "high"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	if got, want := df.Col("grade").Records(), []string{"low", "medium", "NaN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}

	// WithTypes alone also produces a categorical column with inferred levels.
	inferred := LoadRecords([][]string{
		{"grade"}, {"medium"}, {"low"}, {"medium"},
	}, WithTypes(map[string]series.Type{"grade": series.Categorical}))
	if inferred.Err != nil {
		t.Fatalf("unexpected error: %v", inferred.Err)
	}
	if got, want := categoricalLevels(t, inferred, "grade"), []string{"low", "medium"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inferred levels = %v, want %v", got, want)
	}
}

func TestDataFrame_CategoricalDescribe(t *testing.T) {
	df := New(
		series.NewCategorical([]string{"low", "medium", "high"}, "grade",
			series.WithLevels("low", "medium", "high"), series.WithOrdered(true)),
	)
	described := df.Describe()
	if described.Err != nil {
		t.Fatalf("unexpected error: %v", described.Err)
	}
	records := described.Records()
	// min and max rows report labels rather than numeric codes.
	var minRow, maxRow []string
	for _, row := range records[1:] {
		if row[0] == "min" {
			minRow = row
		}
		if row[0] == "max" {
			maxRow = row
		}
	}
	if !reflect.DeepEqual(minRow, []string{"min", "low"}) {
		t.Fatalf("min row = %v", minRow)
	}
	if !reflect.DeepEqual(maxRow, []string{"max", "high"}) {
		t.Fatalf("max row = %v", maxRow)
	}
}
