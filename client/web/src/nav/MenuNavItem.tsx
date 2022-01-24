import MenuDownIcon from 'mdi-react/MenuDownIcon'
import MenuIcon from 'mdi-react/MenuIcon'
import MenuUpIcon from 'mdi-react/MenuUpIcon'
import React from 'react'

import { Menu, MenuButton, MenuItem, Popover, PopoverContent } from '@sourcegraph/wildcard'

interface MenuNavItemProps {
    children: React.ReactNode
    openByDefault?: boolean
}

/**
 * Displays a dropdown menu in the navbar
 * displaiyng navigation links as menu items
 *
 */

export const MenuNavItem: React.FunctionComponent<MenuNavItemProps> = ({ children }) => (
    <Menu>
        {({ isExpanded }) => (
            <>
                <MenuButton className="bg-transparent">
                    <MenuIcon className="icon-inline" color="blue" />
                    {isExpanded ? <MenuUpIcon className="icon-inline" /> : <MenuDownIcon className="icon-inline" />}
                </MenuButton>
                <Popover>
                    <PopoverContent>
                        {React.Children.map(
                            children,
                            child => child && <MenuItem onSelect={() => {}}>{child}</MenuItem>
                        )}
                    </PopoverContent>
                </Popover>
            </>
        )}
    </Menu>
)
