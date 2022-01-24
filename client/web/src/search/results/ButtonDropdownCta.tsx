import classNames from 'classnames'
import React, { useEffect, useLayoutEffect, useCallback, useRef } from 'react'

import { TelemetryProps } from '@sourcegraph/shared/src/telemetry/telemetryService'
import { Button, Menu, MenuButton, useOpenMenuButton, Link, Popover, PopoverContent } from '@sourcegraph/wildcard'

import { CloudSignUpSource } from '../../auth/CloudSignUpPage'

import styles from './ButtonDropdownCta.module.scss'

export interface ButtonDropdownCtaProps extends TelemetryProps {
    button: JSX.Element
    icon: JSX.Element
    title: string
    copyText: string
    source: CloudSignUpSource
    viewEventName: string
    returnTo: string
    onToggle?: () => void
    className?: string
}

export const ButtonDropdownCta: React.FunctionComponent<ButtonDropdownCtaProps> = ({
    button,
    icon,
    title,
    copyText,
    telemetryService,
    source,
    viewEventName,
    returnTo,
    onToggle,
    className,
}) => {
    const menuButtonReference = useRef<HTMLButtonElement>(null)
    const { setIsOverButton, isDropdownOpen, setIsDropdownOpen } = useOpenMenuButton(menuButtonReference)

    const toggleDropdownOpen = useCallback(() => {
        setIsDropdownOpen(isOpen => !isOpen)
        onToggle?.()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [onToggle])

    useLayoutEffect(() => {
        toggleDropdownOpen()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [isDropdownOpen])
    const onClick = (): void => {
        telemetryService.log(`SignUpPLG${source}_1_Search`)
    }

    // Whenever dropdown opens, log view event
    useEffect(() => {
        telemetryService.log(viewEventName)
        onToggle?.()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [onToggle])

    return (
        <Menu>
            {() => (
                <>
                    <MenuButton
                        className={classNames(
                            'btn btn-sm btn-outline-secondary text-decoration-none menu-nav-item',
                            className,
                            styles.toggle
                        )}
                        ref={menuButtonReference}
                        onClick={() => {
                            setIsDropdownOpen(!isDropdownOpen)
                        }}
                        onMouseEnter={() => {
                            setIsOverButton(true)
                        }}
                        onMouseLeave={() => {
                            setIsOverButton(false)
                        }}
                    >
                        {button}
                    </MenuButton>
                    <Popover>
                        <PopoverContent className={styles.container}>
                            <div className="d-flex mb-3">
                                <div className="d-flex align-items-center mr-3">
                                    <div className={styles.icon}>{icon}</div>
                                </div>
                                <div>
                                    <div className={styles.title}>
                                        <strong>{title}</strong>
                                    </div>
                                    <div className={classNames('text-muted', styles.copyText)}>{copyText}</div>
                                </div>
                            </div>
                            <Button
                                to={`/sign-up?src=${source}&returnTo=${encodeURIComponent(returnTo)}`}
                                onClick={onClick}
                                variant="primary"
                                as={Link}
                            >
                                Sign up for Sourcegraph
                            </Button>
                        </PopoverContent>
                    </Popover>
                </>
            )}
        </Menu>
    )
}
