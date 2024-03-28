package built_in_usage_calculators_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBuiltInUsageCalculators(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BuiltInUsageCalculators Suite")
}
