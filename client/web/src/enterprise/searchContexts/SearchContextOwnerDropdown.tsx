import classNames from 'classnames'
import MenuDownIcon from 'mdi-react/MenuDownIcon'
import React, { useMemo } from 'react'

import { Namespace } from '@sourcegraph/shared/src/schema'
import { Menu, MenuButton, MenuItem, Popover, PopoverContent } from '@sourcegraph/wildcard'

import { AuthenticatedUser } from '../../auth'

import styles from './SearchContextOwnerDropdown.module.scss'

export type SelectedNamespaceType = 'user' | 'org' | 'global-owner'

export interface SelectedNamespace {
    id: string | null
    type: SelectedNamespaceType
    name: string
}

export function getSelectedNamespace(namespace: Namespace | null): SelectedNamespace {
    if (!namespace) {
        return { id: null, type: 'global-owner', name: '' }
    }
    return {
        id: namespace.id,
        type: namespace.__typename === 'User' ? 'user' : 'org',
        name: namespace.namespaceName,
    }
}

export function getSelectedNamespaceFromUser(authenticatedUser: AuthenticatedUser): SelectedNamespace {
    return {
        id: authenticatedUser.id,
        type: 'user',
        name: authenticatedUser.username,
    }
}

export interface SearchContextOwnerDropdownProps {
    isDisabled: boolean
    authenticatedUser: AuthenticatedUser
    selectedNamespace: SelectedNamespace
    setSelectedNamespace: (selectedNamespace: SelectedNamespace) => void
}

export const SearchContextOwnerDropdown: React.FunctionComponent<SearchContextOwnerDropdownProps> = ({
    isDisabled,
    authenticatedUser,
    selectedNamespace,
    setSelectedNamespace,
}) => {
    const selectedUserNamespace = useMemo(() => getSelectedNamespaceFromUser(authenticatedUser), [authenticatedUser])
    return (
        <Menu>
            {() => (
                <>
                    <MenuButton
                        className={classNames('form-control', styles.searchContextOwnerDropdownToggle)}
                        color="outline-secondary"
                        disabled={isDisabled}
                        data-tooltip={isDisabled ? "Owner can't be changed." : ''}
                    >
                        <div>
                            {selectedNamespace.type === 'global-owner' ? 'Global' : `@${selectedNamespace.name}`}{' '}
                            {/* MenuButton does not have an icon by default */}
                            <MenuDownIcon className="icon-inline" />
                        </div>
                    </MenuButton>
                    <Popover>
                        <PopoverContent>
                            <MenuItem onSelect={() => setSelectedNamespace(selectedUserNamespace)}>
                                @{authenticatedUser.username} <span className="text-muted">(you)</span>
                            </MenuItem>
                            {authenticatedUser.organizations.nodes.map(org => (
                                <MenuItem
                                    key={org.name}
                                    onSelect={() => setSelectedNamespace({ id: org.id, type: 'org', name: org.name })}
                                >
                                    @{org.name}
                                </MenuItem>
                            ))}
                            {authenticatedUser.siteAdmin && (
                                <>
                                    <hr />
                                    <MenuItem
                                        onSelect={() =>
                                            setSelectedNamespace({ id: null, type: 'global-owner', name: '' })
                                        }
                                    >
                                        <div>Global owner</div>
                                        <div className="text-muted">Available to everyone.</div>
                                    </MenuItem>
                                </>
                            )}
                        </PopoverContent>
                    </Popover>
                </>
            )}
        </Menu>
    )
}
