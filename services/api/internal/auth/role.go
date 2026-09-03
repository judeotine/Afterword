package auth

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

var roleRank = map[Role]int{
	RoleMember: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
}

func (r Role) Valid() bool {
	_, ok := roleRank[r]
	return ok
}

func (r Role) AtLeast(minimum Role) bool {
	have, ok := roleRank[r]
	if !ok {
		return false
	}
	want, ok := roleRank[minimum]
	if !ok {
		return false
	}
	return have >= want
}

func ParseRole(value string) (Role, error) {
	role := Role(value)
	if !role.Valid() {
		return "", ErrInvalidRole
	}
	return role, nil
}
