package db_test

import (
	"database/sql"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
	"github.com/lib/pq"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Leases", func() {
	var (
		dbConn   *sql.DB
		listener *pq.Listener

		pipelineDBFactory db.PipelineDBFactory
		sqlDB             *db.SQLDB

		pipelineDB db.PipelineDB
	)

	BeforeEach(func() {
		postgresRunner.CreateTestDB()

		dbConn = postgresRunner.Open()

		listener = pq.NewListener(postgresRunner.DataSourceName(), time.Second, time.Minute, nil)
		Eventually(listener.Ping, 5*time.Second).ShouldNot(HaveOccurred())
		bus := db.NewNotificationsBus(listener, dbConn)

		sqlDB = db.NewSQL(lagertest.NewTestLogger("test"), dbConn, bus)
		pipelineDBFactory = db.NewPipelineDBFactory(lagertest.NewTestLogger("test"), dbConn, bus, sqlDB)
	})

	AfterEach(func() {
		err := dbConn.Close()
		Ω(err).ShouldNot(HaveOccurred())

		err = listener.Close()
		Ω(err).ShouldNot(HaveOccurred())

		postgresRunner.DropTestDB()
	})

	pipelineConfig := atc.Config{
		Resources: atc.ResourceConfigs{
			{
				Name: "some-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
		},
	}

	BeforeEach(func() {
		_, err := sqlDB.SaveConfig("pipeline-name", pipelineConfig, 0, db.PipelineUnpaused)
		Ω(err).ShouldNot(HaveOccurred())

		savedPipeline, err := sqlDB.GetPipelineByName("pipeline-name")
		Ω(err).ShouldNot(HaveOccurred())

		pipelineDB = pipelineDBFactory.Build(savedPipeline)
	})

	Describe("taking out a lease on pipeline scheduling", func() {
		Context("when it has been scheduled recently", func() {
			It("does not get the lease", func() {
				lease, leased, err := pipelineDB.LeaseScheduling(1 * time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				lease.Break()

				_, leased, err = pipelineDB.LeaseScheduling(1 * time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeFalse())
			})
		})

		Context("when there has not been any scheduling recently", func() {
			It("gets and keeps the lease and stops others from getting it", func() {
				lease, leased, err := pipelineDB.LeaseScheduling(1 * time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				Consistently(func() bool {
					_, leased, err = pipelineDB.LeaseScheduling(1 * time.Second)
					Ω(err).ShouldNot(HaveOccurred())

					return leased
				}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

				lease.Break()

				time.Sleep(time.Second)

				newLease, leased, err := pipelineDB.LeaseScheduling(1 * time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				newLease.Break()
			})
		})
	})

	Describe("taking out a lease on resource checking", func() {
		BeforeEach(func() {
			_, err := pipelineDB.GetResource("some-resource")
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when there has been a check recently", func() {
			Context("when acquiring immediately", func() {
				It("gets the lease", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					lease.Break()

					lease, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					lease.Break()
				})
			})

			Context("when not acquiring immediately", func() {
				It("does not get the lease", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					lease.Break()

					_, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeFalse())
				})
			})
		})

		Context("when there has not been a check recently", func() {
			Context("when acquiring immediately", func() {
				It("gets and keeps the lease and stops others from periodically getting it", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					Consistently(func() bool {
						_, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
						Ω(err).ShouldNot(HaveOccurred())

						return leased
					}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

					lease.Break()

					time.Sleep(time.Second)

					newLease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					newLease.Break()
				})

				It("gets and keeps the lease and stops others from immediately getting it", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					Consistently(func() bool {
						_, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
						Ω(err).ShouldNot(HaveOccurred())

						return leased
					}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

					lease.Break()

					time.Sleep(time.Second)

					newLease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					newLease.Break()
				})
			})

			Context("when not acquiring immediately", func() {
				It("gets and keeps the lease and stops others from periodically getting it", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					Consistently(func() bool {
						_, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
						Ω(err).ShouldNot(HaveOccurred())

						return leased
					}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

					lease.Break()

					time.Sleep(time.Second)

					newLease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					newLease.Break()
				})

				It("gets and keeps the lease and stops others from immediately getting it", func() {
					lease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					Consistently(func() bool {
						_, leased, err = pipelineDB.LeaseCheck("some-resource", 1*time.Second, true)
						Ω(err).ShouldNot(HaveOccurred())

						return leased
					}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

					lease.Break()

					time.Sleep(time.Second)

					newLease, leased, err := pipelineDB.LeaseCheck("some-resource", 1*time.Second, false)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(leased).Should(BeTrue())

					newLease.Break()
				})
			})
		})
	})

	Describe("taking out a lease on build tracking", func() {
		var buildID int

		BeforeEach(func() {
			build, err := sqlDB.CreateOneOffBuild()
			Ω(err).ShouldNot(HaveOccurred())

			buildID = build.ID
		})

		Context("when something has been tracking it recently", func() {
			It("does not get the lease", func() {
				lease, leased, err := sqlDB.LeaseTrack(buildID, 1*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				lease.Break()

				_, leased, err = sqlDB.LeaseTrack(buildID, 1*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeFalse())
			})
		})

		Context("when there has not been any tracking recently", func() {
			It("gets and keeps the lease and stops others from getting it", func() {
				lease, leased, err := sqlDB.LeaseTrack(buildID, 1*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				Consistently(func() bool {
					_, leased, err = sqlDB.LeaseTrack(buildID, 1*time.Second)
					Ω(err).ShouldNot(HaveOccurred())

					return leased
				}, 1500*time.Millisecond, 100*time.Millisecond).Should(BeFalse())

				lease.Break()

				time.Sleep(time.Second)

				newLease, leased, err := sqlDB.LeaseTrack(buildID, 1*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(leased).Should(BeTrue())

				newLease.Break()
			})
		})
	})
})