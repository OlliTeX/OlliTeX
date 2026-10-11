/**
 * review-panel-message: edited-timestamp display (TODO-9f4614a0, upstream
 * overleaf#1509 — commit 3ca22077 on
 * origin <https://github.com/BetterAndBetterII/overleaf.git> branch
 * cursor/comment-edited-timestamp-cf24).
 *
 * A message that has been edited (wire `edited_at`, epoch millis) shows:
 *   - the EDITED time in the header (not the original creation time)
 *   - the "(edited)" marker next to it
 * The original `timestamp` remains the sort key (creation order is a
 * threads-context concern, asserted separately in e2e).
 */
import { render, screen } from '@testing-library/react'
import { expect } from 'chai'
import { UserContext } from '@/shared/context/user-context'
import { PermissionsContext } from '@/features/ide-react/context/permissions-context'
import { ReviewPanelMessage } from '../../../../../js/features/review-panel/components/review-panel-message'
import type {
  CommentId,
  ReviewPanelCommentThreadMessage,
} from '../../../../../types/review-panel/review-panel'

const author = {
  id: 'user-1',
  name: 'Owner',
  email: 'owner@example.test',
  avatar_text: 'O',
  hue: 100,
  isSelf: true,
}

function messageWith(overrides: Partial<ReviewPanelCommentThreadMessage>): ReviewPanelCommentThreadMessage {
  return {
    content: 'Hello',
    id: 'msg-1' as CommentId,
    // original creation time
    timestamp: new Date(2024, 0, 5, 10, 30, 0),
    user: author,
    user_id: 'user-1',
    ...overrides,
  }
}

function renderMessage(message: ReviewPanelCommentThreadMessage) {
  return render(
    <UserContext.Provider value={author as never}>
      <PermissionsContext.Provider
        value={{
          comment: true,
          resolveAllComments: true,
          resolveOwnComments: true,
        } as never}
      >
        <ReviewPanelMessage
          message={message}
          hasReplies={false}
          isReply={false}
          isThreadResolved={false}
        />
      </PermissionsContext.Provider>
    </UserContext.Provider>
  )
}

describe('<ReviewPanelMessage /> edited timestamp', function () {
  it('shows the original time without a marker when never edited', function () {
    renderMessage(messageWith({}))

    // 5 January 2024, 10:30 am (cross-year format: `D MMMM YYYY, h:mm a`)
    expect(screen.getByText(/5 January 2024/)).to.exist
    expect(screen.queryByText('(edited)')).to.be.null
  })

  it('shows the EDITED time and the (edited) marker when edited_at is set', function () {
    // edited one day after creation, 09:00
    renderMessage(messageWith({ edited_at: new Date(2024, 0, 6, 9, 0, 0).getTime() }))

    expect(screen.getByText(/6 January 2024/)).to.exist
    expect(screen.getByText('(edited)')).to.exist
    // the original creation time must NOT be the displayed one
    expect(screen.queryByText(/5 January 2024/)).to.be.null
  })
})
