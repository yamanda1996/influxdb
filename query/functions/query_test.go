package functions_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/influxdata/flux"
	"github.com/influxdata/flux/csv"
	"github.com/influxdata/flux/execute/executetest"
	ifql "github.com/influxdata/flux/influxql"
	"github.com/influxdata/flux/lang"
	"github.com/influxdata/flux/memory"
	"github.com/influxdata/flux/querytest"
	"github.com/influxdata/platform"
	"github.com/influxdata/platform/mock"
	"github.com/influxdata/platform/query"
	_ "github.com/influxdata/platform/query/builtin"
	"github.com/influxdata/platform/query/influxql"
	platformtesting "github.com/influxdata/platform/testing"

	"github.com/andreyvit/diff"
)

const generatedInfluxQLDataDir = "testdata"

var dbrpMappingSvc = mock.NewDBRPMappingService()

func init() {
	mapping := platform.DBRPMapping{
		Cluster:         "cluster",
		Database:        "db0",
		RetentionPolicy: "autogen",
		Default:         true,
		OrganizationID:  platformtesting.MustIDBase16("cadecadecadecade"),
		BucketID:        platformtesting.MustIDBase16("da7aba5e5eedca5e"),
	}
	dbrpMappingSvc.FindByFn = func(ctx context.Context, cluster string, db string, rp string) (*platform.DBRPMapping, error) {
		return &mapping, nil
	}
	dbrpMappingSvc.FindFn = func(ctx context.Context, filter platform.DBRPMappingFilter) (*platform.DBRPMapping, error) {
		return &mapping, nil
	}
	dbrpMappingSvc.FindManyFn = func(ctx context.Context, filter platform.DBRPMappingFilter, opt ...platform.FindOptions) ([]*platform.DBRPMapping, int, error) {
		return []*platform.DBRPMapping{&mapping}, 1, nil
	}
}

var skipTests = map[string]string{
	"hardcoded_literal_1":      "transpiler count query is off by 1 (https://github.com/influxdata/platform/issues/1278)",
	"hardcoded_literal_3":      "transpiler count query is off by 1 (https://github.com/influxdata/platform/issues/1278)",
	"fuzz_join_within_cursor":  "transpiler does not implement joining fields within a cursor (https://github.com/influxdata/platform/issues/1340)",
	"derivative_count":         "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_first":         "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_last":          "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_max":           "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_mean":          "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_median":        "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_min":           "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_mode":          "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_percentile_10": "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_percentile_50": "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_percentile_90": "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
	"derivative_sum":           "add derivative support to the transpiler (https://github.com/influxdata/platform/issues/93)",
}

var querier = querytest.NewQuerier()

func withEachFluxFile(t testing.TB, fn func(prefix, caseName string)) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "testdata")

	fluxFiles, err := filepath.Glob(filepath.Join(path, "*.flux"))
	if err != nil {
		t.Fatalf("error searching for Flux files: %s", err)
	}

	for _, fluxFile := range fluxFiles {
		ext := filepath.Ext(fluxFile)
		prefix := fluxFile[0 : len(fluxFile)-len(ext)]
		_, caseName := filepath.Split(prefix)
		fn(prefix, caseName)
	}
}

func withEachInfluxQLFile(t testing.TB, fn func(prefix, caseName string)) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, generatedInfluxQLDataDir)

	influxqlFiles, err := filepath.Glob(filepath.Join(path, "*.influxql"))
	if err != nil {
		t.Fatalf("error searching for influxQL files: %s", err)
	}

	for _, influxqlFile := range influxqlFiles {
		ext := filepath.Ext(influxqlFile)
		prefix := influxqlFile[0 : len(influxqlFile)-len(ext)]
		_, caseName := filepath.Split(prefix)
		fn(prefix, caseName)
	}
}

func Test_QueryEndToEnd(t *testing.T) {
	withEachFluxFile(t, func(prefix, caseName string) {
		reason, skip := skipTests[caseName]

		fluxName := caseName + ".flux"
		influxqlName := caseName + ".influxql"
		t.Run(fluxName, func(t *testing.T) {
			if skip {
				t.Skip(reason)
			}
			testFlux(t, querier, prefix, ".flux")
		})
		t.Run(influxqlName, func(t *testing.T) {
			if skip {
				t.Skip(reason)
			}
			testInfluxQL(t, querier, prefix, ".influxql")
		})
	})
}

func Test_GeneratedInfluxQLQueries(t *testing.T) {
	withEachInfluxQLFile(t, func(prefix, caseName string) {
		reason, skip := skipTests[caseName]
		influxqlName := caseName + ".influxql"
		t.Run(influxqlName, func(t *testing.T) {
			if skip {
				t.Skip(reason)
			}
			testGeneratedInfluxQL(t, prefix, ".influxql")
		})
	})
}

func Benchmark_QueryEndToEnd(b *testing.B) {
	withEachFluxFile(b, func(prefix, caseName string) {
		reason, skip := skipTests[caseName]
		if skip {
			b.Skip(reason)
		}

		fluxName := caseName + ".flux"
		influxqlName := caseName + ".influxql"
		b.Run(fluxName, func(b *testing.B) {
			if skip {
				b.Skip(reason)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				testFlux(b, querier, prefix, ".flux")
			}
		})
		b.Run(influxqlName, func(b *testing.B) {
			if skip {
				b.Skip(reason)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				testInfluxQL(b, querier, prefix, ".influxql")
			}
		})
	})
}

func testFlux(t testing.TB, querier *querytest.Querier, prefix, queryExt string) {
	q, err := ioutil.ReadFile(prefix + queryExt)
	if err != nil {
		t.Fatal(err)
	}

	csvInFilename := prefix + ".in.csv"
	csvOut, err := ioutil.ReadFile(prefix + ".out.csv")
	if err != nil {
		t.Fatal(err)
	}

	compiler := lang.FluxCompiler{
		Query: string(q),
	}
	req := &query.ProxyRequest{
		Request: query.Request{
			Compiler: querytest.FromCSVCompiler{
				Compiler:  compiler,
				InputFile: csvInFilename,
			},
		},
		Dialect: csv.DefaultDialect(),
	}

	QueryTestCheckSpec(t, querier, req, string(csvOut))
}

func testInfluxQL(t testing.TB, querier *querytest.Querier, prefix, queryExt string) {
	q, err := ioutil.ReadFile(prefix + queryExt)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Skip("influxql query is missing")
	}

	csvInFilename := prefix + ".in.csv"
	csvOut, err := ioutil.ReadFile(prefix + ".out.csv")
	if err != nil {
		t.Fatal(err)
	}

	compiler := influxql.NewCompiler(dbrpMappingSvc)
	compiler.Cluster = "cluster"
	compiler.DB = "db0"
	compiler.Query = string(q)
	req := &query.ProxyRequest{
		Request: query.Request{
			Compiler: querytest.FromCSVCompiler{
				Compiler:  compiler,
				InputFile: csvInFilename,
			},
		},
		Dialect: csv.DefaultDialect(),
	}
	QueryTestCheckSpec(t, querier, req, string(csvOut))

	// Rerun test for InfluxQL JSON dialect
	req.Dialect = new(influxql.Dialect)

	jsonOut, err := ioutil.ReadFile(prefix + ".out.json")
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Skip("influxql expected json is missing")
	}
	QueryTestCheckSpec(t, querier, req, string(jsonOut))
}

func testGeneratedInfluxQL(t testing.TB, prefix, queryExt string) {
	q, err := ioutil.ReadFile(prefix + queryExt)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Skip("influxql query is missing")
	}

	inFile := prefix + ".in.json"
	outFile := prefix + ".out.json"

	out, err := jsonToResultIterator(outFile)
	if err != nil {
		t.Fatalf("failed to read expected JSON results: %v", err)
	}
	defer out.Release()

	var exp []flux.Result
	for out.More() {
		exp = append(exp, out.Next())
	}

	res, err := resultsFromQuerier(querier, influxQLCompiler(string(q), inFile))
	if err != nil {
		t.Fatalf("failed to run query: %v", err)
	}
	defer res.Release()

	var got []flux.Result
	for res.More() {
		got = append(got, res.Next())
	}

	if ok, err := executetest.EqualResults(exp, got); !ok {
		t.Errorf("result not as expected: %v", err)
	}
}

func resultsFromQuerier(querier *querytest.Querier, compiler flux.Compiler) (flux.ResultIterator, error) {
	req := &query.ProxyRequest{
		Request: query.Request{
			Compiler: compiler,
		},
		Dialect: new(influxql.Dialect),
	}
	jsonBuf, err := queryToJSON(querier, req)
	if err != nil {
		return nil, err
	}
	decoder := ifql.NewResultDecoder(new(memory.Allocator))
	return decoder.Decode(ioutil.NopCloser(jsonBuf))
}

func influxQLCompiler(query, filename string) querytest.FromInfluxJSONCompiler {
	compiler := influxql.NewCompiler(dbrpMappingSvc)
	compiler.Cluster = "cluster"
	compiler.DB = "db0"
	compiler.Query = query
	return querytest.FromInfluxJSONCompiler{
		Compiler:  compiler,
		InputFile: filename,
	}
}

func queryToJSON(querier *querytest.Querier, req *query.ProxyRequest) (io.ReadCloser, error) {
	var buf bytes.Buffer
	_, err := querier.Query(context.Background(), &buf, req.Request.Compiler, req.Dialect)
	if err != nil {
		return nil, err
	}
	return ioutil.NopCloser(&buf), nil
}

func jsonToResultIterator(file string) (flux.ResultIterator, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	// Reader for influxql json file
	jsonReader := bufio.NewReaderSize(f, 8196)

	// InfluxQL json -> Flux tables decoder
	decoder := ifql.NewResultDecoder(new(memory.Allocator))

	// Decode json into Flux tables
	results, err := decoder.Decode(ioutil.NopCloser(jsonReader))
	if err != nil {
		return nil, err
	}
	return results, nil
}

func QueryTestCheckSpec(t testing.TB, querier *querytest.Querier, req *query.ProxyRequest, want string) {
	t.Helper()

	var buf bytes.Buffer
	_, err := querier.Query(context.Background(), &buf, req.Request.Compiler, req.Dialect)
	if err != nil {
		t.Errorf("failed to run query: %v", err)
		return
	}

	got := buf.String()

	if g, w := strings.TrimSpace(got), strings.TrimSpace(want); g != w {
		t.Errorf("result not as expected want(-) got (+):\n%v", diff.LineDiff(w, g))
	}
}
