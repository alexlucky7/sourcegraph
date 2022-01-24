import { cloneDeep } from 'lodash'
import { BehaviorSubject, Observable } from 'rxjs'

import { SearchQueryState } from '@sourcegraph/search'
import { AuthenticatedUser } from '@sourcegraph/shared/src/auth'
import { AggregateStreamingSearchResults } from '@sourcegraph/shared/src/search/stream'

// State management in the Sourcegraph VS Code extension
// -----
// This extension runs code in 4 (and counting) different execution contexts.
// Coordinating state between these contexts is a difficult task.
// So, instead of managing shared state in each context, we maintain
// one state machine in the "Core" context (see './extension.ts' for architecure diagram).
// All contexts listen for state updates and emit events on which the state
// machine may transition.
// For example:
// - Commands from VS Code extension core
// - The first submitted search in a session will cause the state machine
//    to transition from the `search-home` state to the `search-results` state.
//    This new state will be reflected in both the search sidebar and search panel UIs
//

// We represent a hierarchical state machine in a "flat" manner to reduce code complexity
// and because our state machine is simple enough to not necessitate bringing in a library.
// So,
//   ┌───►home
//   │
// - search
//   │
//   └───►results
// - remote-browsing
// - idle
// - context-invalidated
// becomes:
// - [search-home, search-results, remote-browsing, idle, context-invalidated]

// Example user flow state transitions:
// - User clicks on Sourcegraph logo in VS Code sidebar.
// - Extension activates with initial state of `search-home`
// - User submits search -> state === `search-results`
// - User clicks on a search result, which opens a file -> state === `remote-browsing`
// - User copies some code, then focuses an editor for a local file -> state === `idle`

export interface VSCEStateMachine {
    state: VSCEState
    /**
     * Returns an Observable that emits the current state and
     * on subsequent state updates.
     */
    observeState: () => Observable<VSCEState>
    emit: (event: VSCEEvent) => void
}
export type VSCEState = SearchHomeState | SearchResultsState | RemoteBrowsingState | IdleState | ContextInvalidatedState

export interface SearchHomeState {
    status: 'search-home'
    context: CommonContext
}

export interface SearchResultsState {
    status: 'search-results'
    context: CommonContext & {
        submittedSearchQueryState: Pick<SearchQueryState, 'queryState' | 'searchCaseSensitivity' | 'searchPatternType'>
    }
}
export interface RemoteBrowsingState {
    status: 'remote-browsing'
    context: CommonContext
}

export interface IdleState {
    status: 'idle'
    context: CommonContext
}

export interface ContextInvalidatedState {
    status: 'context-invalidated'
    context: CommonContext & {
        reason: 'url-change' | 'access-token-change'
    }
}
interface CommonContext {
    authenticatedUser: AuthenticatedUser | null

    // The search sidebar only needs submitted search query state.
    // The search panel maintains its own local query state.
    submittedSearchQueryState: Pick<
        SearchQueryState,
        'queryState' | 'searchCaseSensitivity' | 'searchPatternType'
    > | null

    // In common context and not just `search-results` to retain during `idle` or `remote-browsing` states.
    searchResults: AggregateStreamingSearchResults | null

    // TODO: search context (and persist.)
}

const INITIAL_STATE: VSCEState = {
    status: 'search-home',
    context: {
        authenticatedUser: null,
        submittedSearchQueryState: null,
        searchResults: null,
    },
}

// Temporary placeholder events. We will replace these with the actual events as we implement the webviews.

export type VSCEEvent = SearchEvent | TabsEvent | SettingsEvent

type SearchEvent =
    | { type: 'set_query_state' }
    | {
          type: 'submit_search_query'
          submittedSearchQueryState: NonNullable<CommonContext['submittedSearchQueryState']>
      }
    | { type: 'received_search_results'; searchResults: AggregateStreamingSearchResults }

type TabsEvent =
    | { type: 'search_panel_unfocused' }
    | { type: 'search_panel_focused' }
    | { type: 'remote_file_focused' }
    | { type: 'remote_file_unfocused' }

type SettingsEvent =
    | {
          type: 'sourcegraph_url_change'
      }
    | { type: 'access_token_change' }

export function createVSCEStateMachine(): VSCEStateMachine {
    const states = new BehaviorSubject<VSCEState>(INITIAL_STATE)

    function reducer(state: VSCEState, event: VSCEEvent): VSCEState {
        // End state.
        if (state.status === 'context-invalidated') {
            return state
        }

        // Events with the same behavior regardless of current state
        if (event.type === 'sourcegraph_url_change' || event.type === 'access_token_change') {
            return {
                status: 'context-invalidated',
                context: {
                    ...INITIAL_STATE.context,
                    reason: event.type === 'sourcegraph_url_change' ? 'url-change' : 'access-token-change',
                },
            }
        }

        switch (state.status) {
            case 'search-home':
            case 'search-results':
                switch (event.type) {
                    case 'submit_search_query':
                        return {
                            status: 'search-results',
                            context: {
                                ...state.context,
                                submittedSearchQueryState: event.submittedSearchQueryState,
                                searchResults: null, // Null out previous results.
                            },
                        }

                    case 'received_search_results': {
                        // Only accept this event in search-results state.
                        if (state.status === 'search-results') {
                            return {
                                status: 'search-results',
                                context: {
                                    ...state.context,
                                    searchResults: event.searchResults,
                                },
                            }
                        }
                    }

                    case 'search_panel_unfocused':
                        return {
                            ...state,
                            status: 'idle',
                        }

                    case 'remote_file_focused':
                        return {
                            ...state,
                            status: 'remote-browsing',
                        }
                }
                return state

            case 'remote-browsing':
                switch (event.type) {
                    case 'search_panel_focused': {
                        if (state.context.submittedSearchQueryState) {
                            return {
                                status: 'search-results',
                                context: {
                                    ...state.context,
                                    submittedSearchQueryState: state.context.submittedSearchQueryState,
                                },
                            }
                        }

                        return {
                            ...state,
                            status: 'search-home',
                        }
                    }
                    case 'remote_file_unfocused':
                        return {
                            ...state,
                            status: 'idle',
                        }
                }

                return state

            case 'idle':
                switch (event.type) {
                    case 'search_panel_focused': {
                        if (state.context.submittedSearchQueryState) {
                            return {
                                status: 'search-results',
                                context: {
                                    ...state.context,
                                    submittedSearchQueryState: state.context.submittedSearchQueryState,
                                },
                            }
                        }

                        return {
                            ...state,
                            status: 'search-home',
                        }
                    }

                    case 'remote_file_focused':
                        return {
                            ...state,
                            status: 'remote-browsing',
                        }
                }

                return state
        }
    }

    return {
        get state() {
            return cloneDeep(states.value)
        },
        observeState: () => states.asObservable(),
        emit: event => {
            const nextState = reducer(states.value, event)
            states.next(nextState)
        },
    }
}
