// S4/D25 (OT sweep F2): the OT offline-recovery path (OfflineDocBackup +
// UnableToSyncModal on doc sync failure) was retired with the OT substrate.
// The contract this spec now pins:
//  * a document sync error fails closed with the OutOfSyncModal;
//  * the ide:unableToSyncOfflineChanges / ide:offlineChangesSynced events
//    (still typed on the IDE event emitter) continue to drive the
//    UnableToSyncModal and the success toast, so a future recovery source
//    (e.g. D28a bus era) can re-attach UI without new plumbing.
import { FC, PropsWithChildren, ReactNode, useEffect, useState } from 'react'
import EventEmitter from '@/utils/EventEmitter'
import { useEditorManagerContext } from '@/features/ide-react/context/editor-manager-context'
import { useIdeReactContext } from '@/features/ide-react/context/ide-react-context'
import { IdeEventEmitter } from '@/features/ide-react/create-ide-event-emitter'
import { GlobalToasts } from '@/features/ide-react/components/global-toasts'
import { location } from '@/shared/components/location'
import {
  EditorProviders,
  makeEditorOpenDocProvider,
  PROJECT_ID,
  USER_ID,
} from '../../../helpers/editor-providers'

const CURRENT_DOC_ID = 'current-doc'
const NEW_DOC_ID = 'new-doc'

class FakeDocumentContainer extends EventEmitter {
  doc_id = CURRENT_DOC_ID
  docName = 'main.tex'
  doc = { clearInflightAndPendingOps: cy.stub() }

  getSnapshot() {
    return 'server snapshot'
  }
  hasBufferedOps() {
    return false
  }
  leaveAndCleanUp() {}
  leaveAndCleanUpPromise() {
    return Promise.resolve()
  }
}

const OpenNewDocOnMount: FC = () => {
  const editorManager = useEditorManagerContext()
  useEffect(() => {
    editorManager.openDoc({ _id: NEW_DOC_ID } as any).catch(() => {})
  }, [editorManager])
  return null
}

const CaptureEventEmitter: FC<{
  onReady: (emitter: IdeEventEmitter) => void
}> = ({ onReady }) => {
  const { eventEmitter } = useIdeReactContext()
  useEffect(() => {
    onReady(eventEmitter)
  }, [eventEmitter, onReady])
  return null
}

describe('EditorManagerProvider docError sync modals (post-OT D25/F2)', function () {
  let currentDoc: FakeDocumentContainer

  beforeEach(function () {
    currentDoc = new FakeDocumentContainer()
    cy.then(() => {
      window.metaAttributesCache.set('ol-user_id', USER_ID)
    })
  })

  const mount = (children: ReactNode = null) => {
    cy.mount(
      <EditorProviders
        projectId={PROJECT_ID}
        providers={{
          EditorOpenDocProvider: makeEditorOpenDocProvider({
            currentDocumentId: CURRENT_DOC_ID as any,
            openDocName: currentDoc.docName,
            currentDocument: currentDoc as any,
          }),
        }}
      >
        {children}
      </EditorProviders>
    )
  }

  it('fails closed with the OutOfSyncModal on a document sync error', function () {
    mount(
      <CaptureEventEmitter onReady={() => {}} />
    )

    cy.then(() => {
      currentDoc.trigger('error', new Error('forced'), {}, 'content')
    })

    cy.findByRole('dialog').should('exist')
    cy.findByText('Your offline edits couldn’t be synced').should('not.exist')
  })

  it('shows UnableToSyncModal on the ide:unableToSyncOfflineChanges recovery event', function () {
    let capturedEmitter: IdeEventEmitter | null = null

    mount(
      <CaptureEventEmitter
        onReady={emitter => {
          capturedEmitter = emitter
        }}
      />
    )

    cy.then(() => {
      capturedEmitter!.emit('ide:unableToSyncOfflineChanges', {
        docId: CURRENT_DOC_ID,
        editorContent: 'recovered content',
        baseContent: 'original content',
        docName: 'recovered.tex',
      })
    })

    cy.findByRole('dialog').within(() => {
      cy.findByText('Your offline edits couldn’t be synced').should('exist')
    })
  })

  it('does not reload when the recovery event omits reloadAfterClose', function () {
    let capturedEmitter: IdeEventEmitter | null = null

    cy.then(() => {
      cy.stub(location, 'reload').as('reload')
    })

    mount(
      <CaptureEventEmitter
        onReady={emitter => {
          capturedEmitter = emitter
        }}
      />
    )

    cy.then(() => {
      capturedEmitter!.emit('ide:unableToSyncOfflineChanges', {
        docId: CURRENT_DOC_ID,
        editorContent: 'recovered content',
        baseContent: 'original content',
        docName: 'recovered.tex',
      })
    })

    cy.findByRole('dialog').within(() => {
      cy.findByRole('button', { name: 'Discard changes' }).click()
    })

    cy.get('@reload').should('not.have.been.called')
  })

  it('shows the success toast on the ide:offlineChangesSynced event', function () {
    let capturedEmitter: IdeEventEmitter | null = null

    cy.mount(
      <EditorProviders
        projectId={PROJECT_ID}
        providers={{
          EditorOpenDocProvider: makeEditorOpenDocProvider({
            currentDocumentId: CURRENT_DOC_ID as any,
            openDocName: currentDoc.docName,
            currentDocument: currentDoc as any,
          }),
        }}
      >
        <CaptureEventEmitter
          onReady={emitter => {
            capturedEmitter = emitter
          }}
        />
        <GlobalToasts />
      </EditorProviders>
    )

    cy.then(() => {
      capturedEmitter!.emit('ide:offlineChangesSynced', {
        docId: CURRENT_DOC_ID,
      })
    })

    cy.findByText('You’re back online.').should('exist')
  })
})
