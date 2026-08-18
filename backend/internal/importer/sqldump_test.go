package importer

import "testing"

func TestMysqlDumpToCSV_BasicInsert(t *testing.T) {
	dump := "-- MySQL dump 10.13\n" +
		"CREATE TABLE `users` (`id` int, `username` varchar(64));\n" +
		"INSERT INTO `users` (`id`, `username`, `plan`) VALUES\n" +
		"(1, 'ali01', 'Basic'),\n" +
		"(2, 'sara_2', 'Gold Plan');\n"

	res, err := mysqlDumpToCSV(dump, "")
	if err != nil {
		t.Fatalf("mysqlDumpToCSV: %v", err)
	}
	wantHeader := []string{"id", "username", "plan"}
	if len(res.Header) != 3 {
		t.Fatalf("header = %v, want %v", res.Header, wantHeader)
	}
	for i, h := range wantHeader {
		if res.Header[i] != h {
			t.Errorf("header[%d] = %q, want %q", i, res.Header[i], h)
		}
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0][1] != "ali01" || res.Rows[1][1] != "sara_2" {
		t.Errorf("usernames = %v / %v, want ali01 / sara_2", res.Rows[0][1], res.Rows[1][1])
	}
	if res.Rows[1][2] != "Gold Plan" {
		t.Errorf("plan = %q, want %q", res.Rows[1][2], "Gold Plan")
	}
}

func TestMysqlDumpToCSV_EscapedQuotesAndNull(t *testing.T) {
	dump := "INSERT INTO `subs` (`name`, `note`, `phone`) VALUES ('O\\'Brien', NULL, '0770');\n"
	res, err := mysqlDumpToCSV(dump, "")
	if err != nil {
		t.Fatalf("mysqlDumpToCSV: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(res.Rows))
	}
	if res.Rows[0][0] != "O'Brien" {
		t.Errorf("name = %q, want O'Brien", res.Rows[0][0])
	}
	if res.Rows[0][1] != "" {
		t.Errorf("note (NULL) = %q, want empty", res.Rows[0][1])
	}
	if res.Rows[0][2] != "0770" {
		t.Errorf("phone = %q, want 0770", res.Rows[0][2])
	}
}

func TestMysqlDumpToCSV_CommaAndParenInsideQuotedValue(t *testing.T) {
	dump := "INSERT INTO `t` (`name`) VALUES ('Ali (Baghdad, Zone 3)');\n"
	res, err := mysqlDumpToCSV(dump, "")
	if err != nil {
		t.Fatalf("mysqlDumpToCSV: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "Ali (Baghdad, Zone 3)" {
		t.Fatalf("rows = %+v, want one row with the unsplit value", res.Rows)
	}
}

func TestMysqlDumpToCSV_MultipleTablesPicksNamed(t *testing.T) {
	dump := "INSERT INTO `plans` (`name`) VALUES ('Gold');\n" +
		"INSERT INTO `users` (`username`) VALUES ('u1');\n"
	res, err := mysqlDumpToCSV(dump, "users")
	if err != nil {
		t.Fatalf("mysqlDumpToCSV: %v", err)
	}
	if len(res.Header) != 1 || res.Header[0] != "username" {
		t.Fatalf("header = %v, want [username]", res.Header)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "u1" {
		t.Fatalf("rows = %+v, want [[u1]]", res.Rows)
	}
}

func TestMysqlDumpToCSV_NoInsertFound(t *testing.T) {
	if _, err := mysqlDumpToCSV("CREATE TABLE t (id int);\n", ""); err != errNoInsertFound {
		t.Fatalf("err = %v, want errNoInsertFound", err)
	}
}

func TestSplitValueTuples_RespectsQuotesAndParens(t *testing.T) {
	tuples, err := splitValueTuples("(1, 'a,b'), (2, 'c(d)e')")
	if err != nil {
		t.Fatalf("splitValueTuples: %v", err)
	}
	if len(tuples) != 2 {
		t.Fatalf("tuples = %v, want 2", tuples)
	}
	if tuples[0] != "1, 'a,b'" {
		t.Errorf("tuple[0] = %q", tuples[0])
	}
	if tuples[1] != "2, 'c(d)e'" {
		t.Errorf("tuple[1] = %q", tuples[1])
	}
}

func TestWriteCSV_RoundTrip(t *testing.T) {
	res := dumpToCSVResult{Header: []string{"a", "b"}, Rows: [][]string{{"1", "hello, world"}}}
	text, err := writeCSV(res)
	if err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	if text == "" {
		t.Fatal("empty CSV output")
	}
}
