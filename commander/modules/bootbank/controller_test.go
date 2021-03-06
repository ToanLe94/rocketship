package bootbank

import (
	"strings"
	"testing"

	"github.com/amoghe/distillog"
	"github.com/jinzhu/gorm"

	_ "github.com/mattn/go-sqlite3"
	. "gopkg.in/check.v1"
)

type BootbankTestSuite struct {
	db         gorm.DB
	controller *Controller
}

// Register the test suite with gocheck.
func init() {
	Suite(&BootbankTestSuite{})
}

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

func (ts *BootbankTestSuite) SetUpTest(c *C) {
	db, err := gorm.Open("sqlite3", "file::memory:?cache=shared")
	c.Assert(err, IsNil)
	ts.db = db

	ts.controller = NewController(&ts.db, distillog.NewNullLogger("test"))
	ts.controller.MigrateDB()
	ts.controller.SeedDB()
}

func (ts *BootbankTestSuite) TearDownTest(c *C) {
	ts.db.Close()
}

func (ts *BootbankTestSuite) TestGrubConfFileContents(c *C) {
	f, err := ts.controller.grubConfContents(Bootbank1)
	c.Assert(err, IsNil)

	c.Assert(strings.Contains(string(f), "set default=\"Rocketship1\""), Equals, true)
	c.Assert(strings.Count(string(f), "menuentry"), Equals, 2)
}
