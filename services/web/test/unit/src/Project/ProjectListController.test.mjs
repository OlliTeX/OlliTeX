import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import sinon from 'sinon'
import mongodb from 'mongodb-legacy'
import Settings from '@overleaf/settings'
import {
  InvalidRequestError,
  setReqValidationModeForTests,
} from '@overleaf/validation-tools'

const ObjectId = mongodb.ObjectId

const MODULE_PATH = `${import.meta.dirname}/../../../../app/src/Features/Project/ProjectListController`

// Mock AnalyticsManager as it isn't used in these tests but causes the User model to be imported and redeclares queues
vi.mock('../../../../app/src/Features/Analytics/AnalyticsManager.mjs', () => {
  return {
    default: {
      setUserPropertyForUserInBackground: () => {},
      setUserPropertyForSessionInBackground: () => {},
    },
  }
})

describe('ProjectListController', function () {
  beforeEach(async function (ctx) {
    ctx.project_id = new ObjectId('abcdefabcdefabcdefabcdef')

    ctx.user = {
      _id: new ObjectId('123456123456123456123456'),
      email: 'test@overleaf.com',
      first_name: 'bjkdsjfk',
      features: {},
      emails: [{ email: 'test@overleaf.com' }],
      lastActive: new Date(2),
      signUpDate: new Date(1),
      lastLoginIp: '111.111.111.112',
      ace: {
        syntaxValidation: true,
        pdfViewer: 'pdfjs',
        spellCheckLanguage: 'en',
        autoPairDelimiters: true,
        autoComplete: true,
        fontSize: 12,
        theme: 'textmate',
        mode: 'none',
      },
      aiFeatures: { enabled: false },
    }
    ctx.users = {
      'user-1': {
        first_name: 'James',
      },
      'user-2': {
        first_name: 'Henry',
      },
    }
    ctx.users[ctx.user._id] = ctx.user // Owner
    ctx.usersArr = Object.entries(ctx.users).map(([key, value]) => ({
      _id: key,
      ...value,
    }))
    ctx.tags = [
      { name: 1, project_ids: ['1', '2', '3'] },
      { name: 2, project_ids: ['a', '1'] },
      { name: 3, project_ids: ['a', 'b', 'c', 'd'] },
    ]
    ctx.notifications = [
      {
        _id: '1',
        user_id: '2',
        templateKey: '3',
        messageOpts: '4',
        key: '5',
      },
    ]
    ctx.settings = {
      ...Settings,
      siteUrl: 'https://overleaf.com',
    }
    ctx.onboardingDataCollection = {
      companyDivisionDepartment: '',
      companyJobTitle: '',
      firstName: 'Dos',
      governmentJobTitle: '',
      institutionName: '',
      lastName: 'Mukasan',
      nonprofitDivisionDepartment: '',
      nonprofitJobTitle: '',
      otherJobTitle: '',
      primaryOccupation: 'company',
      role: 'conductor',
      subjectArea: 'music',
      updatedAt: '2025-09-04T12:12:21.628Z',
      usedLatex: 'occasionally',
    }
    ctx.TagsHandler = {
      promises: {
        getAllTags: sinon.stub().resolves(ctx.tags),
      },
    }
    ctx.NotificationsHandler = {
      promises: {
        getUserNotifications: sinon.stub().resolves(ctx.notifications),
      },
    }
    ctx.UserModel = {
      findById: sinon.stub().resolves(ctx.user),
    }
    ctx.OnboardingDataCollectionModel = {
      findById: sinon.stub().resolves(ctx.onboardingDataCollection),
    }
    ctx.UserPrimaryEmailCheckHandler = {
      requiresPrimaryEmailCheck: sinon.stub().returns(false),
    }
    ctx.ProjectGetter = {
      promises: {
        findAllUsersProjects: sinon.stub(),
      },
    }
    ctx.ProjectHelper = {
      isArchived: sinon.stub(),
      isTrashed: sinon.stub(),
    }
    ctx.SessionManager = {
      getLoggedInUserId: sinon.stub().returns(ctx.user._id),
    }
    ctx.UserController = {
      logout: sinon.stub(),
    }
    ctx.UserGetter = {
      promises: {
        getUsers: sinon.stub().resolves(ctx.usersArr),
        getUserFullEmails: sinon.stub().resolves([]),
        getWritefullData: sinon.stub().resolves({ isPremium: true }),
      },
    }
    ctx.Features = {
      hasFeature: sinon.stub(),
    }
    ctx.SplitTestHandler = {
      promises: {
        getAssignment: sinon.stub().resolves({ variant: 'default' }),
        featureFlagEnabled: sinon.stub().resolves(false),
        hasUserBeenAssignedToVariant: sinon.stub().resolves(false),
      },
    }
    ctx.SplitTestSessionHandler = {
      promises: {
        sessionMaintenance: sinon.stub().resolves(),
      },
    }
    ctx.SubscriptionViewModelBuilder = {
      promises: {
        getUsersSubscriptionDetails: sinon.stub().resolves({
          bestSubscription: { type: 'free' },
          individualSubscription: null,
          memberGroupSubscriptions: [],
          managedGroupSubscriptions: [],
        }),
      },
    }
    ctx.SurveyHandler = {
      promises: {
        getSurvey: sinon.stub().resolves({}),
      },
    }
    ctx.NotificationBuilder = {
      promises: {
        ipMatcherAffiliation: sinon.stub().returns({ create: sinon.stub() }),
      },
    }
    ctx.GeoIpLookup = {
      promises: {
        getCurrencyCode: sinon.stub().resolves({
          countryCode: 'US',
          currencyCode: 'USD',
        }),
      },
    }
    ctx.TutorialHandler = {
      getInactiveTutorials: sinon.stub().returns([]),
    }

    ctx.Modules = {
      promises: {
        hooks: {
          fire: sinon.stub().resolves([]),
        },
      },
    }

    ctx.PermissionsManager = {
      promises: {
        checkUserPermissions: sinon.stub().resolves(true),
      },
    }

    ctx.SubscriptionLocator = {
      promises: {
        getUsersSubscription: sinon.stub().resolves({}),
      },
    }

    vi.doMock('mongodb-legacy', () => ({
      default: { ObjectId },
    }))

    vi.doMock('@overleaf/settings', () => ({
      default: ctx.settings,
    }))

    vi.doMock(
      '../../../../app/src/Features/SplitTests/SplitTestHandler',
      () => ({
        default: ctx.SplitTestHandler,
      })
    )

    vi.doMock(
      '../../../../app/src/Features/SplitTests/SplitTestSessionHandler',
      () => ({
        default: ctx.SplitTestSessionHandler,
      })
    )

    vi.doMock('../../../../app/src/Features/User/UserController', () => ({
      default: ctx.UserController,
    }))

    vi.doMock('../../../../app/src/Features/Project/ProjectHelper', () => ({
      default: ctx.ProjectHelper,
    }))

    vi.doMock('../../../../app/src/Features/Tags/TagsHandler', () => ({
      default: ctx.TagsHandler,
    }))

    vi.doMock(
      '../../../../app/src/Features/Notifications/NotificationsHandler',
      () => ({
        default: ctx.NotificationsHandler,
      })
    )

    vi.doMock('../../../../app/src/models/User', () => ({
      User: ctx.UserModel,
    }))

    vi.doMock('../../../../app/src/models/OnboardingDataCollection', () => ({
      OnboardingDataCollection: ctx.OnboardingDataCollectionModel,
    }))

    vi.doMock('../../../../app/src/Features/Project/ProjectGetter', () => ({
      default: ctx.ProjectGetter,
    }))

    vi.doMock(
      '../../../../app/src/Features/Authentication/SessionManager',
      () => ({
        default: ctx.SessionManager,
      })
    )

    vi.doMock('../../../../app/src/infrastructure/Features', () => ({
      default: ctx.Features,
    }))

    vi.doMock('../../../../app/src/Features/User/UserGetter', () => ({
      default: ctx.UserGetter,
    }))

    vi.doMock('../../../../app/src/infrastructure/Modules', () => ({
      default: ctx.Modules,
    }))

    vi.doMock(
      '../../../../app/src/Features/User/UserPrimaryEmailCheckHandler',
      () => ({
        default: ctx.UserPrimaryEmailCheckHandler,
      })
    )

    vi.doMock(
      '../../../../app/src/Features/Notifications/NotificationsBuilder',
      () => ({
        default: ctx.NotificationBuilder,
      })
    )

    vi.doMock('../../../../app/src/infrastructure/GeoIpLookup', () => ({
      default: ctx.GeoIpLookup,
    }))

    vi.doMock('../../../../app/src/Features/Tutorial/TutorialHandler', () => ({
      default: ctx.TutorialHandler,
    }))

    vi.doMock(
      '../../../../app/src/Features/Authorization/PermissionsManager',
      () => ({
        default: ctx.PermissionsManager,
      })
    )
    ctx.ProjectListController = (await import(MODULE_PATH)).default

    ctx.req = {
      query: {},
      params: {
        Project_id: ctx.project_id,
      },
      headers: {},
      session: {
        user: ctx.user,
      },
      body: {},
      i18n: {
        translate() {},
      },
    }
    ctx.res = {}
  })

  afterEach(function () {
    setReqValidationModeForTests(null)
  })

  describe('getProjectsJson', function () {
    beforeEach(function (ctx) {
      ctx.projects = [
        { _id: 1, lastUpdated: new Date(1), owner_ref: 'user-1' },
        { _id: 2, lastUpdated: new Date(2), owner_ref: 'user-2' },
      ]
      ctx.allProjects = {
        owned: ctx.projects,
        readAndWrite: [],
        readOnly: [],
        tokenReadAndWrite: [],
        tokenReadOnly: [],
        review: [],
      }
      ctx.ProjectGetter.promises.findAllUsersProjects.resolves(ctx.allProjects)
      ctx.next = sinon.stub()
    })

    it('should respond with the paginated/filtered/sorted projects', async function (ctx) {
      ctx.req.body = {
        filters: { ownedByUser: true },
        sort: { by: 'lastUpdated', order: 'desc' },
        page: { size: 20 },
      }
      await new Promise(resolve => {
        ctx.res.json = data => {
          expect(data.totalSize).to.equal(ctx.projects.length)
          expect(data.projects).to.have.length(ctx.projects.length)
          resolve()
        }
        ctx.ProjectListController.getProjectsJson(ctx.req, ctx.res, ctx.next)
      })
      sinon.assert.notCalled(ctx.next)
    })

    it('should work with only a sort and no filters/page', async function (ctx) {
      ctx.req.body = { sort: { by: 'title', order: 'asc' } }
      await new Promise(resolve => {
        ctx.res.json = data => {
          expect(data.totalSize).to.equal(ctx.projects.length)
          resolve()
        }
        ctx.ProjectListController.getProjectsJson(ctx.req, ctx.res, ctx.next)
      })
    })

    it('should not reject an unsupported sort.by value in log mode', async function (ctx) {
      setReqValidationModeForTests('log')
      ctx.req.body = { sort: { by: 'not-a-real-field', order: 'asc' } }
      await ctx.ProjectListController.getProjectsJson(
        ctx.req,
        ctx.res,
        ctx.next
      )
      sinon.assert.calledOnce(ctx.next)
      const err = ctx.next.firstCall.args[0]
      expect(err).to.not.be.instanceOf(InvalidRequestError)
      expect(err.message).to.equal('Invalid sorting criteria')
    })

    it('should not reject an unknown filter key in log mode', async function (ctx) {
      setReqValidationModeForTests('log')
      ctx.req.body = { filters: { notARealFilter: true } }
      await new Promise(resolve => {
        ctx.res.json = data => {
          expect(data.totalSize).to.equal(ctx.projects.length)
          resolve()
        }
        ctx.ProjectListController.getProjectsJson(ctx.req, ctx.res, ctx.next)
      })
      sinon.assert.notCalled(ctx.next)
    })

    describe('request validation', function () {
      beforeEach(function () {
        setReqValidationModeForTests('enforce')
      })

      it('rejects an unsupported sort.by value', async function (ctx) {
        ctx.req.body = { sort: { by: 'not-a-real-field', order: 'asc' } }
        await ctx.ProjectListController.getProjectsJson(
          ctx.req,
          ctx.res,
          ctx.next
        )
        sinon.assert.calledWith(
          ctx.next,
          sinon.match.instanceOf(InvalidRequestError)
        )
      })

      it('rejects an unknown filter key', async function (ctx) {
        ctx.req.body = { filters: { notARealFilter: true } }
        await ctx.ProjectListController.getProjectsJson(
          ctx.req,
          ctx.res,
          ctx.next
        )
        sinon.assert.calledWith(
          ctx.next,
          sinon.match.instanceOf(InvalidRequestError)
        )
      })
    })
  })
})
