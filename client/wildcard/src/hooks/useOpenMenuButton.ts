import React, { Dispatch, SetStateAction, useLayoutEffect, useState } from 'react'

interface UseOpenMenuButtonProps {
    setIsOverButton: Dispatch<SetStateAction<boolean>>
    isDropdownOpen: boolean
    setIsDropdownOpen: Dispatch<SetStateAction<boolean>>
}

/**
 * A React hook that open and close Menu from '@reach/menu-button'
 * Not only when click event fired but also hovering the button
 *
 * @param reference A MenuButton ref from React.useRef
 * @returns UseOpenMenuButtonProps
 */
export const useOpenMenuButton = (reference: React.RefObject<HTMLButtonElement>): UseOpenMenuButtonProps => {
    const [isDropdownOpen, setIsDropdownOpen] = useState(false)
    const [isOverButton, setIsOverButton] = useState(false)

    useLayoutEffect(() => {
        if (!reference?.current) {
            return
        }
        reference.current?.click()

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [isOverButton, isDropdownOpen])

    return { setIsOverButton, isDropdownOpen, setIsDropdownOpen }
}
