package util

type IntSetFlag IntSet

func (i *IntSetFlag) String() string { return "" }

func (i *IntSetFlag) Set(value string) error {
	set, err := ParseIntSet(value)
	if err != nil {
		return err
	}
	*i = IntSetFlag(*set)
	return nil
}

func (i *IntSetFlag) Contains(value int) bool {
	return (IntSet)(*i).Contains(value)
}
