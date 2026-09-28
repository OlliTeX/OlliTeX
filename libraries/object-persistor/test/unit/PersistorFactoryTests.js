const chai = require('chai')
const { expect } = chai
const SandboxedModule = require('sandboxed-module')
const StreamPromises = require('node:stream/promises')

const MODULE_PATH = '../../src/PersistorFactory.js'

describe('PersistorManager', function () {
  let PersistorFactory, S3Persistor, Settings, GcsPersistor

  beforeEach(function () {
    S3Persistor = class {
      wrappedMethod() {
        return 'S3Persistor'
      }
    }
    GcsPersistor = class {
      wrappedMethod() {
        return 'GcsPersistor'
      }
    }

    Settings = {}
    const requires = {
      './GcsPersistor': GcsPersistor,
      './S3Persistor': { S3Persistor },
      '@overleaf/logger': {
        info() {},
        err() {},
      },
      'stream/promises': StreamPromises,
    }
    PersistorFactory = SandboxedModule.require(MODULE_PATH, { requires })
  })

  it('should implement the S3 wrapped method when S3 is configured', function () {
    Settings.backend = 's3'

    expect(PersistorFactory(Settings)).to.respondTo('wrappedMethod')
    expect(PersistorFactory(Settings).wrappedMethod()).to.equal('S3Persistor')
  })

  it("should implement the S3 wrapped method when 'aws-sdk' is configured", function () {
    Settings.backend = 'aws-sdk'

    expect(PersistorFactory(Settings)).to.respondTo('wrappedMethod')
    expect(PersistorFactory(Settings).wrappedMethod()).to.equal('S3Persistor')
  })

  it('retires the fs backend with an actionable error (G2 STOR-1)', function () {
    Settings.backend = 'fs'
    try {
      PersistorFactory(Settings)
    } catch (err) {
      expect(err.message).to.contain('retired')
      return
    }
    expect('should have caught an error').not.to.exist
  })

  it('should throw an error when the backend is not configured', function () {
    try {
      PersistorFactory(Settings)
    } catch (err) {
      expect(err.message).to.equal('no backend specified - config incomplete')
      return
    }
    expect('should have caught an error').not.to.exist
  })

  it('should throw an error when the backend is unknown', function () {
    Settings.backend = 'magic'
    try {
      PersistorFactory(Settings)
    } catch (err) {
      expect(err.message).to.equal('unknown backend')
      expect(err.info.backend).to.equal('magic')
      return
    }
    expect('should have caught an error').not.to.exist
  })
})
