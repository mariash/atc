package scheduler_test

import (
	"errors"

	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/db/algorithm"
	"github.com/concourse/atc/db/dbfakes"
	. "github.com/concourse/atc/scheduler"
	"github.com/concourse/atc/scheduler/buildstarter/buildstarterfakes"
	"github.com/concourse/atc/scheduler/inputmapper/inputmapperfakes"
	"github.com/concourse/atc/scheduler/schedulerfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduler", func() {
	var (
		fakeDB           *schedulerfakes.FakeSchedulerDB
		fakeInputMapper  *inputmapperfakes.FakeInputMapper
		fakeBuildStarter *buildstarterfakes.FakeBuildStarter
		fakeScanner      *schedulerfakes.FakeScanner

		scheduler *Scheduler

		disaster error
	)

	BeforeEach(func() {
		fakeDB = new(schedulerfakes.FakeSchedulerDB)
		fakeInputMapper = new(inputmapperfakes.FakeInputMapper)
		fakeBuildStarter = new(buildstarterfakes.FakeBuildStarter)
		fakeScanner = new(schedulerfakes.FakeScanner)

		scheduler = &Scheduler{
			DB:           fakeDB,
			InputMapper:  fakeInputMapper,
			BuildStarter: fakeBuildStarter,
			Scanner:      fakeScanner,
		}

		disaster = errors.New("bad thing")
	})

	Describe("Schedule", func() {
		var (
			versionsDB  *algorithm.VersionsDB
			jobConfig   atc.JobConfig
			scheduleErr error
		)

		JustBeforeEach(func() {
			versionsDB = &algorithm.VersionsDB{JobIDs: map[string]int{"j1": 1}}

			var waiter Waiter
			scheduleErr = scheduler.Schedule(
				lagertest.NewTestLogger("test"),
				versionsDB,
				jobConfig,
				atc.ResourceConfigs{{Name: "some-resource"}},
				atc.ResourceTypes{{Name: "some-resource-type"}},
			)
			if waiter != nil {
				waiter.Wait()
			}
		})

		Context("when the job has no inputs", func() {
			BeforeEach(func() {
				jobConfig = atc.JobConfig{Name: "some-job"}
			})

			Context("when saving the next input mapping fails", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(nil, disaster)
				})

				It("returns the error", func() {
					Expect(scheduleErr).To(Equal(disaster))
				})

				It("saved the next input mapping for the right job and versions", func() {
					Expect(fakeInputMapper.SaveNextInputMappingCallCount()).To(Equal(1))
					_, actualVersionsDB, actualJobConfig := fakeInputMapper.SaveNextInputMappingArgsForCall(0)
					Expect(actualVersionsDB).To(Equal(versionsDB))
					Expect(actualJobConfig).To(Equal(jobConfig))
				})
			})

			Context("when saving the next input mapping succeeds", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(algorithm.InputMapping{}, nil)
				})

				Context("when starting all pending builds fails", func() {
					BeforeEach(func() {
						fakeBuildStarter.TryStartAllPendingBuildsReturns(disaster)
					})

					It("returns the error", func() {
						Expect(scheduleErr).To(Equal(disaster))
					})

					It("started all pending builds for the right job", func() {
						Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
						_, actualJob, actualResources, actualResourceTypes := fakeBuildStarter.TryStartAllPendingBuildsArgsForCall(0)
						Expect(actualJob).To(Equal(jobConfig))
						Expect(actualResources).To(Equal(atc.ResourceConfigs{{Name: "some-resource"}}))
						Expect(actualResourceTypes).To(Equal(atc.ResourceTypes{{Name: "some-resource-type"}}))
					})
				})

				Context("when starting all pending builds succeeds", func() {
					BeforeEach(func() {
						fakeBuildStarter.TryStartAllPendingBuildsReturns(nil)
					})

					It("returns no error", func() {
						Expect(scheduleErr).NotTo(HaveOccurred())
					})

					It("didn't create a pending build", func() {
						Expect(fakeDB.EnsurePendingBuildExistsCallCount()).To(BeZero())
					})
				})
			})
		})

		Context("when the job has one trigger: true input", func() {
			BeforeEach(func() {
				jobConfig = atc.JobConfig{
					Name: "some-job",
					Plan: atc.PlanSequence{
						{Get: "a", Trigger: true},
						{Get: "b", Trigger: false},
					},
				}

				fakeBuildStarter.TryStartAllPendingBuildsReturns(nil)
			})

			Context("when no input mapping is found", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(algorithm.InputMapping{}, nil)
				})

				It("starts all pending builds and returns no error", func() {
					Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
					Expect(scheduleErr).NotTo(HaveOccurred())
				})

				It("didn't create a pending build", func() {
					Expect(fakeDB.EnsurePendingBuildExistsCallCount()).To(BeZero())
				})
			})

			Context("when no first occurrence input has trigger: true", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(algorithm.InputMapping{
						"a": algorithm.InputVersion{VersionID: 1, FirstOccurrence: false},
						"b": algorithm.InputVersion{VersionID: 2, FirstOccurrence: true},
					}, nil)
				})

				It("starts all pending builds and returns no error", func() {
					Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
					Expect(scheduleErr).NotTo(HaveOccurred())
				})

				It("didn't create a pending build", func() {
					Expect(fakeDB.EnsurePendingBuildExistsCallCount()).To(BeZero())
				})
			})

			Context("when a first occurrence input has trigger: true", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(algorithm.InputMapping{
						"a": algorithm.InputVersion{VersionID: 1, FirstOccurrence: true},
						"b": algorithm.InputVersion{VersionID: 2, FirstOccurrence: false},
					}, nil)
				})

				Context("when creating a pending build fails", func() {
					BeforeEach(func() {
						fakeDB.EnsurePendingBuildExistsReturns(disaster)
					})

					It("returns the error", func() {
						Expect(scheduleErr).To(Equal(disaster))
					})

					It("created a pending build for the right job", func() {
						Expect(fakeDB.EnsurePendingBuildExistsCallCount()).To(Equal(1))
						Expect(fakeDB.EnsurePendingBuildExistsArgsForCall(0)).To(Equal("some-job"))
					})
				})

				Context("when creating a pending build succeeds", func() {
					BeforeEach(func() {
						fakeDB.EnsurePendingBuildExistsReturns(nil)
					})

					It("starts all pending builds and returns no error", func() {
						Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
						Expect(scheduleErr).NotTo(HaveOccurred())
					})
				})
			})
		})
	})

	Describe("TriggerImmediately", func() {
		var (
			jobConfig      atc.JobConfig
			triggeredBuild db.Build
			triggerErr     error
		)

		JustBeforeEach(func() {
			jobConfig = atc.JobConfig{Name: "some-job", Plan: atc.PlanSequence{{Get: "input-1"}, {Get: "input-2"}}}

			var waiter Waiter
			triggeredBuild, waiter, triggerErr = scheduler.TriggerImmediately(
				lagertest.NewTestLogger("test"),
				jobConfig,
				atc.ResourceConfigs{{Name: "some-resource"}},
				atc.ResourceTypes{{Name: "some-resource-type"}})
			if waiter != nil {
				waiter.Wait()
			}
		})

		Context("when getting the lock errors", func() {
			BeforeEach(func() {
				fakeDB.AcquireResourceCheckingForJobLockReturns(nil, false, disaster)
			})

			It("returns the error", func() {
				Expect(triggerErr).To(Equal(disaster))
			})

			It("asked for the lock for the right job", func() {
				Expect(fakeDB.AcquireResourceCheckingForJobLockCallCount()).To(Equal(1))
				_, actualJobName := fakeDB.AcquireResourceCheckingForJobLockArgsForCall(0)
				Expect(actualJobName).To(Equal("some-job"))
			})
		})

		Context("when someone else is holding the lock", func() {
			BeforeEach(func() {
				fakeDB.AcquireResourceCheckingForJobLockReturns(nil, false, nil)
			})

			Context("when creating the build fails", func() {
				BeforeEach(func() {
					fakeDB.CreateJobBuildReturns(nil, disaster)
				})

				It("returns the error", func() {
					Expect(triggerErr).To(Equal(disaster))
				})

				It("created a build for the right job", func() {
					Expect(fakeDB.CreateJobBuildCallCount()).To(Equal(1))
					Expect(fakeDB.CreateJobBuildArgsForCall(0)).To(Equal("some-job"))
				})
			})

			Context("when creating the build succeeds", func() {
				var createdBuild *dbfakes.FakeBuild

				BeforeEach(func() {
					createdBuild = new(dbfakes.FakeBuild)
					fakeDB.CreateJobBuildStub = func(jobName string) (db.Build, error) {
						defer GinkgoRecover()
						Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(BeZero())
						return createdBuild, nil
					}
				})

				Context("when starting all pending builds fails", func() {
					BeforeEach(func() {
						fakeBuildStarter.TryStartAllPendingBuildsReturns(disaster)
					})

					It("tried to start all pending builds after creating the build", func() {
						Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
						_, actualJob, actualResources, actualResourceTypes := fakeBuildStarter.TryStartAllPendingBuildsArgsForCall(0)
						Expect(actualJob).To(Equal(jobConfig))
						Expect(actualResources).To(Equal(atc.ResourceConfigs{{Name: "some-resource"}}))
						Expect(actualResourceTypes).To(Equal(atc.ResourceTypes{{Name: "some-resource-type"}}))
					})

					It("returns the build", func() {
						Expect(triggerErr).NotTo(HaveOccurred())
						Expect(triggeredBuild).To(Equal(createdBuild))
					})
				})

				Context("when starting all pending builds succeeds", func() {
					BeforeEach(func() {
						fakeBuildStarter.TryStartAllPendingBuildsReturns(nil)
					})

					It("returns the build", func() {
						Expect(triggerErr).NotTo(HaveOccurred())
						Expect(triggeredBuild).To(Equal(createdBuild))
					})
				})
			})
		})

		Context("when it gets the lock", func() {
			var fakeLease *dbfakes.FakeLease

			BeforeEach(func() {
				fakeLease = new(dbfakes.FakeLease)
				fakeDB.AcquireResourceCheckingForJobLockStub = func(lager.Logger, string) (db.Lock, bool, error) {
					defer GinkgoRecover()
					Expect(fakeDB.CreateJobBuildCallCount()).To(BeZero())
					return fakeLease, true, nil
				}
			})

			Context("when creating the build fails", func() {
				BeforeEach(func() {
					fakeDB.CreateJobBuildReturns(nil, disaster)
				})

				It("returns the error", func() {
					Expect(triggerErr).To(Equal(disaster))
				})

				It("created a build for the right job after acquiring the lock", func() {
					Expect(fakeDB.CreateJobBuildCallCount()).To(Equal(1))
					Expect(fakeDB.CreateJobBuildArgsForCall(0)).To(Equal("some-job"))
				})

				It("releases the lock", func() {
					Expect(fakeLease.BreakCallCount()).To(Equal(1))
				})
			})

			Context("when creating the build succeeds", func() {
				var createdBuild *dbfakes.FakeBuild

				BeforeEach(func() {
					createdBuild = new(dbfakes.FakeBuild)
					fakeDB.CreateJobBuildReturns(createdBuild, nil)
				})

				Context("when resource checking fails", func() {
					BeforeEach(func() {
						fakeScanner.ScanReturns(disaster)
					})

					It("releases the lock and returns the build", func() {
						Expect(triggerErr).NotTo(HaveOccurred())
						Expect(triggeredBuild).To(Equal(createdBuild))
						Expect(fakeLease.BreakCallCount()).To(Equal(1))
					})
				})

				Context("when resource checking succeeds", func() {
					BeforeEach(func() {
						fakeScanner.ScanStub = func(lager.Logger, string) error {
							defer GinkgoRecover()
							Expect(fakeDB.LoadVersionsDBCallCount()).To(BeZero())
							return nil
						}
					})

					Context("when loading the versions DB fails", func() {
						BeforeEach(func() {
							fakeDB.LoadVersionsDBReturns(nil, disaster)
						})

						It("releases the lock and returns the build", func() {
							Expect(triggerErr).NotTo(HaveOccurred())
							Expect(triggeredBuild).To(Equal(createdBuild))
							Expect(fakeLease.BreakCallCount()).To(Equal(1))
						})

						It("checked for the right resources", func() {
							Expect(fakeScanner.ScanCallCount()).To(Equal(2))
							_, resource1 := fakeScanner.ScanArgsForCall(0)
							_, resource2 := fakeScanner.ScanArgsForCall(1)
							Expect([]string{resource1, resource2}).To(ConsistOf("input-1", "input-2"))
						})

						It("loaded the versions DB after checking all the resources", func() {
							Expect(fakeDB.LoadVersionsDBCallCount()).To(Equal(1))
						})
					})

					Context("when loading the versions DB succeeds", func() {
						var versionsDB *algorithm.VersionsDB

						BeforeEach(func() {
							versionsDB = &algorithm.VersionsDB{JobIDs: map[string]int{"j1": 1}}
							fakeDB.LoadVersionsDBReturns(versionsDB, nil)
						})

						Context("when saving the next input mapping fails", func() {
							BeforeEach(func() {
								fakeInputMapper.SaveNextInputMappingReturns(nil, disaster)
							})

							It("releases the lock and returns the build", func() {
								Expect(triggerErr).NotTo(HaveOccurred())
								Expect(triggeredBuild).To(Equal(createdBuild))
								Expect(fakeLease.BreakCallCount()).To(Equal(1))
							})

							It("saved the next input mapping for the right job and versions", func() {
								Expect(fakeInputMapper.SaveNextInputMappingCallCount()).To(Equal(1))
								_, actualVersionsDB, actualJobConfig := fakeInputMapper.SaveNextInputMappingArgsForCall(0)
								Expect(actualVersionsDB).To(Equal(versionsDB))
								Expect(actualJobConfig).To(Equal(jobConfig))
							})
						})

						Context("when saving the next input mapping succeeds", func() {
							BeforeEach(func() {
								fakeInputMapper.SaveNextInputMappingStub = func(lager.Logger, *algorithm.VersionsDB, atc.JobConfig) (algorithm.InputMapping, error) {
									defer GinkgoRecover()
									Expect(fakeLease.BreakCallCount()).To(BeZero())
									return nil, nil
								}
							})

							Context("when starting all pending builds fails", func() {
								BeforeEach(func() {
									fakeBuildStarter.TryStartAllPendingBuildsStub = func(lager.Logger, atc.JobConfig, atc.ResourceConfigs, atc.ResourceTypes) error {
										defer GinkgoRecover()
										Expect(fakeLease.BreakCallCount()).To(Equal(1))
										return disaster
									}
								})

								It("saved the next input mapping before breaking the lock and returns the build", func() {
									Expect(triggerErr).NotTo(HaveOccurred())
									Expect(triggeredBuild).To(Equal(createdBuild))
									Expect(fakeLease.BreakCallCount()).NotTo(BeZero())
								})

								It("started all pending builds for the right job after breaking the lock", func() {
									Expect(fakeBuildStarter.TryStartAllPendingBuildsCallCount()).To(Equal(1))
									_, actualJob, actualResources, actualResourceTypes := fakeBuildStarter.TryStartAllPendingBuildsArgsForCall(0)
									Expect(actualJob).To(Equal(jobConfig))
									Expect(actualResources).To(Equal(atc.ResourceConfigs{{Name: "some-resource"}}))
									Expect(actualResourceTypes).To(Equal(atc.ResourceTypes{{Name: "some-resource-type"}}))
								})
							})

							Context("when starting all pending builds succeeds", func() {
								BeforeEach(func() {
									fakeBuildStarter.TryStartAllPendingBuildsReturns(nil)
								})

								It("returns the build", func() {
									Expect(triggerErr).NotTo(HaveOccurred())
									Expect(triggeredBuild).To(Equal(createdBuild))
								})
							})
						})
					})
				})
			})
		})
	})

	Describe("SaveNextInputMapping", func() {
		var saveErr error

		JustBeforeEach(func() {
			saveErr = scheduler.SaveNextInputMapping(lagertest.NewTestLogger("test"), atc.JobConfig{Name: "some-job"})
		})

		Context("when loading the versions DB fails", func() {
			BeforeEach(func() {
				fakeDB.LoadVersionsDBReturns(nil, disaster)
			})

			It("returns the error", func() {
				Expect(saveErr).To(Equal(disaster))
			})
		})

		Context("when loading the versions DB succeeds", func() {
			var versionsDB *algorithm.VersionsDB

			BeforeEach(func() {
				versionsDB = &algorithm.VersionsDB{JobIDs: map[string]int{"j1": 1}}
				fakeDB.LoadVersionsDBReturns(versionsDB, nil)
			})

			Context("when saving the next input mapping fails", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(nil, disaster)
				})

				It("returns the error", func() {
					Expect(saveErr).To(Equal(disaster))
				})

				It("saved the next input mapping for the right job and versions", func() {
					Expect(fakeInputMapper.SaveNextInputMappingCallCount()).To(Equal(1))
					_, actualVersionsDB, actualJobConfig := fakeInputMapper.SaveNextInputMappingArgsForCall(0)
					Expect(actualVersionsDB).To(Equal(versionsDB))
					Expect(actualJobConfig).To(Equal(atc.JobConfig{Name: "some-job"}))
				})
			})

			Context("when saving the next input mapping succeeds", func() {
				BeforeEach(func() {
					fakeInputMapper.SaveNextInputMappingReturns(algorithm.InputMapping{
						"some-input": algorithm.InputVersion{VersionID: 1, FirstOccurrence: true},
					}, nil)
				})

				It("returns no error", func() {
					Expect(saveErr).NotTo(HaveOccurred())
				})
			})
		})
	})
})
