package wslt

func removeSliceInt64(rs *[]int64, val int64) {
	var r int
	sl := *rs
	for i, v := range sl {
		if v == val {
			r = i
		}
	}
	*rs = append(sl[:r], sl[r+1:]...)
}
