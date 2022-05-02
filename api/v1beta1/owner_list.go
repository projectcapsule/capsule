package v1beta1

import (
	"sort"
)

type OwnerListSpec []OwnerSpec

func (o OwnerListSpec) FindOwner(name string, kind OwnerKind) (owner OwnerSpec) {
	sort.Sort(ByKindAndName(o))
	i := sort.Search(len(o), func(i int) bool {
		return o[i].Kind >= kind && o[i].Name >= name
	})

	if i < len(o) && o[i].Kind == kind && o[i].Name == name {
		return o[i]
	}

	return
}

type ByKindAndName OwnerListSpec

func (b ByKindAndName) Len() int {
	return len(b)
}

func (b ByKindAndName) Less(i, j int) bool {
	if b[i].Kind.String() != b[j].Kind.String() {
		return b[i].Kind.String() < b[j].Kind.String()
	}

	return b[i].Name < b[j].Name
}

func (b ByKindAndName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
