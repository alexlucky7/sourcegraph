import * as H from 'history'
import React from 'react'

import { Hoverifier } from '@sourcegraph/codeintellify'
import { RepoSpec, RevisionSpec, FileSpec, ResolvedRevisionSpec } from '@sourcegraph/common/src/util/url'
import { ActionItemAction } from '@sourcegraph/shared/src/actions/ActionItem'
import { HoverMerged } from '@sourcegraph/shared/src/api/client/types/hover'
import { ExtensionsControllerProps } from '@sourcegraph/shared/src/extensions/controller'
import { ThemeProps } from '@sourcegraph/shared/src/theme'

import { ChangesetFields } from '../../../graphql-operations'
import { queryExternalChangesetWithFileDiffs } from '../detail/backend'

import styles from './ChangesetCloseNode.module.scss'
import { ExternalChangesetCloseNode } from './ExternalChangesetCloseNode'
import { HiddenExternalChangesetCloseNode } from './HiddenExternalChangesetCloseNode'

export interface ChangesetCloseNodeProps extends ThemeProps {
    node: ChangesetFields
    viewerCanAdminister: boolean
    history: H.History
    location: H.Location
    extensionInfo?: {
        hoverifier: Hoverifier<RepoSpec & RevisionSpec & FileSpec & ResolvedRevisionSpec, HoverMerged, ActionItemAction>
    } & ExtensionsControllerProps
    queryExternalChangesetWithFileDiffs?: typeof queryExternalChangesetWithFileDiffs
    willClose: boolean
}

export const ChangesetCloseNode: React.FunctionComponent<ChangesetCloseNodeProps> = ({ node, ...props }) => {
    if (node.__typename === 'ExternalChangeset') {
        return (
            <>
                <span className={styles.changesetCloseNodeSeparator} />
                <ExternalChangesetCloseNode node={node} {...props} />
            </>
        )
    }
    return (
        <>
            <span className={styles.changesetCloseNodeSeparator} />
            <HiddenExternalChangesetCloseNode node={node} {...props} />
        </>
    )
}
