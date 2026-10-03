import { beforeEach, describe, expect, it, vi } from 'vitest'
import sinon from 'sinon'

const modulePath =
  '../../../../app/src/Features/Templates/TemplatesController.mjs'

describe('TemplatesController', function () {
  beforeEach(async function (ctx) {
    ctx.user_id = 'user-id'

    ctx.ProjectHelper = {
      compilerFromV1Engine: sinon.stub(),
    }

    vi.doMock('../../../../app/src/Features/Project/ProjectHelper', () => ({
      default: ctx.ProjectHelper,
    }))

    vi.doMock(
      '../../../../app/src/Features/Authentication/AuthenticationController',
      () => ({
        default: (ctx.AuthenticationController = {
          getLoggedInUserId: sinon.stub().returns(ctx.user_id),
        }),
      })
    )

    vi.doMock(
      '../../../../app/src/Features/Templates/TemplatesManager',
      () => ({
        default: (ctx.TemplatesManager = {
          promises: { createProjectFromV1Template: sinon.stub() },
        }),
      })
    )

    vi.doMock(
      '../../../../app/src/Features/SplitTests/SplitTestHandler',
      () => ({
        default: (ctx.SplitTestHandler = {
          promises: {
            getAssignment: sinon.stub().resolves({ variant: 'default' }),
          },
        }),
      })
    )

    ctx.TemplatesController = (await import(modulePath)).default
    ctx.next = sinon.stub()
    ctx.req = {
      body: {
        brandVariationId: '789',
        compiler: 'compiler',
        mainFile: 'main-file',
        templateId: '123',
        templateName: 'template-name',
        templateVersionId: '456',
      },
      session: {
        templateData: 'template-data',
        user: {
          _id: ctx.user_id,
        },
      },
    }
    return (ctx.res = {
      redirect: sinon.stub(),
      sendStatus: sinon.stub(),
      render: sinon.stub(),
    })
  })

  describe('createProjectFromV1Template', function () {
    describe('on success', function () {
      beforeEach(function (ctx) {
        ctx.project = { _id: 'project-id' }
        ctx.TemplatesManager.promises.createProjectFromV1Template.resolves(
          ctx.project
        )
        return ctx.TemplatesController.createProjectFromV1Template(
          ctx.req,
          ctx.res,
          ctx.next
        )
      })

      it('should call TemplatesManager', function (ctx) {
        return ctx.TemplatesManager.promises.createProjectFromV1Template.should.have.been.calledWithMatch(
          789,
          'compiler',
          'main-file',
          '123',
          'template-name',
          '456',
          'user-id'
        )
      })

      it('should redirect to project', function (ctx) {
        return ctx.res.redirect.should.have.been.calledWith(
          '/project/project-id'
        )
      })

      it('should delete session', function (ctx) {
        return expect(ctx.req.session.templateData).to.be.undefined
      })
    })


    describe('on error', function () {
      beforeEach(function (ctx) {
        ctx.TemplatesManager.promises.createProjectFromV1Template.rejects(
          'error'
        )
        return ctx.TemplatesController.createProjectFromV1Template(
          ctx.req,
          ctx.res,
          ctx.next
        )
      })

      it('should call next with error', function (ctx) {
        return ctx.next.should.have.been.calledWithMatch(
          sinon.match.instanceOf(Error)
        )
      })

      it('should not redirect', function (ctx) {
        return ctx.res.redirect.called.should.equal(false)
      })
    })
  })

  describe('getV1Template', function () {
    beforeEach(function (ctx) {
      ctx.req.params = { Template_version_id: '456' }
    })

    it('should render the template page for valid ids', async function (ctx) {
      ctx.req.query = {
        version: '123',
        name: 'template-name',
        compiler: 'pdflatex',
      }
      await ctx.TemplatesController.getV1Template(ctx.req, ctx.res, ctx.next)
      ctx.next.called.should.equal(false)
      ctx.res.render.should.have.been.calledWithMatch(sinon.match.string, {
        templateVersionId: '123',
        templateId: '456',
        name: 'template-name',
        compiler: 'pdflatex',
      })
    })





  })
})
