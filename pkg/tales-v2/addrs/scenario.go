package addrs

// Scenario struct
type Scenario struct {
	referenceable
	Name string
}

func (s Scenario) String() string {
	return s.Name
}

// Equal returns true if o is equal to s
func (s Scenario) Equal(o Scenario) bool {
	return s.String() == o.String()
}
