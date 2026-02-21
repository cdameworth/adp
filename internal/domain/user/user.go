// Package user provides user management domain logic.
package user

// UserRole represents a user's role in the system.
type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

// UserStatus represents a user's account status.
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

// ValidRole returns true if the given string is a valid user role.
func ValidRole(role string) bool {
	switch UserRole(role) {
	case UserRoleAdmin, UserRoleUser:
		return true
	}
	return false
}

// ValidStatus returns true if the given string is a valid user status.
func ValidStatus(status string) bool {
	switch UserStatus(status) {
	case UserStatusActive, UserStatusDisabled:
		return true
	}
	return false
}
