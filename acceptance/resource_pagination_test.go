package acceptance_test

import (
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/sclevine/agouti"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/sclevine/agouti/matchers"

	"code.cloudfoundry.org/gunk/urljoiner"
	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
)

var _ = Describe("Resource Pagination", func() {
	var atcCommand *ATCCommand
	var dbListener *pq.Listener
	var pipelineDB db.PipelineDB
	var teamID int

	BeforeEach(func() {
		postgresRunner.Truncate()
		dbConn = db.Wrap(postgresRunner.Open())
		dbListener = pq.NewListener(postgresRunner.DataSourceName(), time.Second, time.Minute, nil)
		bus := db.NewNotificationsBus(dbListener, dbConn)

		leaseFactory := db.NewLeaseFactory(postgresRunner.OpenPgx())
		sqlDB = db.NewSQL(dbConn, bus, leaseFactory)

		atcCommand = NewATCCommand(atcBin, 1, postgresRunner.DataSourceName(), []string{}, BASIC_AUTH)
		err := atcCommand.Start()
		Expect(err).NotTo(HaveOccurred())

		teamDBFactory := db.NewTeamDBFactory(dbConn, bus, leaseFactory)
		teamDB := teamDBFactory.GetTeamDB(atc.DefaultTeamName)
		team, found, err := teamDB.GetTeam()
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		teamID = team.ID

		// job build data
		_, _, err = teamDB.SaveConfig(atc.DefaultPipelineName, atc.Config{
			Jobs: atc.JobConfigs{
				{
					Name: "job-name",
					Plan: atc.PlanSequence{
						{
							Get: "resource-name",
						},
					},
				},
			},
			Resources: atc.ResourceConfigs{
				{Name: "resource-name"},
			},
		}, db.ConfigVersion(1), db.PipelineUnpaused)
		Expect(err).NotTo(HaveOccurred())

		savedPipeline, found, err := teamDB.GetPipelineByName(atc.DefaultPipelineName)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		pipelineDBFactory := db.NewPipelineDBFactory(dbConn, bus, leaseFactory)
		pipelineDB = pipelineDBFactory.Build(savedPipeline)
	})

	AfterEach(func() {
		atcCommand.Stop()

		Expect(dbConn.Close()).To(Succeed())
		Expect(dbListener.Close()).To(Succeed())
	})

	Describe("pages", func() {
		var page *agouti.Page

		BeforeEach(func() {
			var err error
			page, err = agoutiDriver.NewPage()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(page.Destroy()).To(Succeed())
		})

		homepage := func() string {
			return atcCommand.URL("")
		}

		withPath := func(path string) string {
			return urljoiner.Join(homepage(), path)
		}

		Context("with more than 100 resource versions", func() {
			BeforeEach(func() {
				resourceConfig := atc.ResourceConfig{
					Name:   "resource-name",
					Type:   "some-type",
					Source: atc.Source{},
				}

				var resourceVersions []atc.Version
				for i := 0; i < 104; i++ {
					resourceVersions = append(resourceVersions, atc.Version{"version": strconv.Itoa(i)})
				}

				err := pipelineDB.SaveResourceVersions(resourceConfig, resourceVersions)
				Expect(err).NotTo(HaveOccurred())
			})

			It("there is pagination", func() {
				// homepage -> resource detail
				Expect(page.Navigate(homepage())).To(Succeed())
				Login(page, homepage())

				Eventually(page.FindByLink("resource-name")).Should(BeFound())
				Expect(page.FindByLink("resource-name").Click()).To(Succeed())

				// resource detail -> paused resource detail
				Eventually(page).Should(HaveURL(withPath("/teams/main/pipelines/main/resources/resource-name")))
				Expect(page.Find("h1")).To(HaveText("resource-name"))
				Expect(page.All(".pagination").Count()).Should(Equal(1))
				Expect(page.Find(".resource-versions")).Should(BeFound())
				Expect(page.All(".resource-versions li").Count()).Should(Equal(100))

				Expect(page.First(".pagination .disabled .fa-arrow-left")).Should(BeFound())
				Expect(page.First(".pagination .fa-arrow-right").Click()).To(Succeed())
				Eventually(page.All(".resource-versions li").Count).Should(Equal(4))

				Expect(page.First(".pagination .disabled .fa-arrow-right")).Should(BeFound())
				Expect(page.First(".pagination .fa-arrow-left").Click()).To(Succeed())
				Eventually(page.All(".resource-versions li").Count).Should(Equal(100))
			})
		})

		Context("with less than 100 resource versions", func() {
			BeforeEach(func() {
				resourceConfig := atc.ResourceConfig{
					Name:   "resource-name",
					Type:   "some-type",
					Source: atc.Source{},
				}

				var resourceVersions []atc.Version
				for i := 0; i < 99; i++ {
					resourceVersions = append(resourceVersions, atc.Version{"version": strconv.Itoa(i)})
				}

				err := pipelineDB.SaveResourceVersions(resourceConfig, resourceVersions)
				Expect(err).NotTo(HaveOccurred())
			})

			It("shows disabled pagination buttons", func() {
				// homepage -> resource detail
				Expect(page.Navigate(homepage())).To(Succeed())
				Login(page, homepage())

				Eventually(page.FindByLink("resource-name")).Should(BeFound())
				Expect(page.FindByLink("resource-name").Click()).To(Succeed())

				// resource detail -> paused resource detail
				Eventually(page).Should(HaveURL(withPath("/teams/main/pipelines/main/resources/resource-name")))
				Expect(page.Find("h1")).To(HaveText("resource-name"))
				Expect(page.First(".pagination .disabled .fa-arrow-left")).Should(BeFound())
				Expect(page.First(".pagination .disabled .fa-arrow-right")).Should(BeFound())
				Expect(page.Find(".resource-versions")).Should(BeFound())
				Expect(page.All(".resource-versions li").Count()).Should(Equal(99))
			})
		})
	})
})
