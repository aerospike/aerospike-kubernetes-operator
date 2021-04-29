// +build test

package asconfig

// Export symbols to be used in testing.

// GetAdminPolicy returns the admin policy to use for access control client calls.
var GetAdminPolicy = getAdminPolicy

// PredefinedRoles is the list of predefined aerospike roles.
var PredefinedRoles = predefinedRoles

// SliceSubtract removes items present in one slice from the other slice.
var SliceSubtract = sliceSubtract

// AerospikePrivilegeToPrivilegeString converts Aerospike privilege into a string.
var AerospikePrivilegeToPrivilegeString = aerospikePrivilegeToPrivilegeString
