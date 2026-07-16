package chronicle

import "testing"

func TestEstimateRowBytes(t *testing.T) {
	// budget + per value: len + 3 + len/10
	got := estimateRowBytes([]string{"aaaaaaaaaa", ""}) // 10+3+1, 0+3+0
	want := jsonRowStructureBudget + 14 + 3
	if got != want {
		t.Errorf("estimateRowBytes = %d, want %d", got, want)
	}
}

func TestBatchRowsByteBudget(t *testing.T) {
	row := []string{"0123456789"} // estimateRowBytes = budget + 14
	rb := estimateRowBytes(row)
	rows := [][]string{row, row, row}
	batches := batchRows(rows, 2*rb) // two rows fit, the third overflows
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("batches = %v (want [2 1] split)", lens(batches))
	}
}

func TestBatchRowsOversizedRowAlone(t *testing.T) {
	small := []string{"x"}
	big := []string{string(make([]byte, 4096))}
	batches := batchRows([][]string{small, big, small}, 100)
	if len(batches) != 3 {
		t.Fatalf("batches = %v, want each row alone", lens(batches))
	}
}

func TestBatchRowsRowCap(t *testing.T) {
	rows := make([][]string, maxRowsPerBatch+2)
	for i := range rows {
		rows[i] = []string{"v"}
	}
	batches := batchRows(rows, 1<<30)
	if len(batches) != 2 || len(batches[0]) != maxRowsPerBatch || len(batches[1]) != 2 {
		t.Fatalf("batches = %v, want [%d 2]", lens(batches), maxRowsPerBatch)
	}
}

func TestBatchRowsEmpty(t *testing.T) {
	if batches := batchRows(nil, 1000); batches != nil {
		t.Errorf("batchRows(nil) = %v, want nil", batches)
	}
}

func TestBatchRowUpdatesSizesByValues(t *testing.T) {
	u := RowUpdate{Values: []string{"0123456789"}}
	rb := estimateRowBytes(u.Values)
	batches := batchRowUpdates([]RowUpdate{u, u, u}, 2*rb)
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("update batches have wrong shape: %d", len(batches))
	}
}

func lens[T any](batches [][]T) []int {
	out := make([]int, len(batches))
	for i, b := range batches {
		out[i] = len(b)
	}
	return out
}
