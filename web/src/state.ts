import { ref } from 'vue'

// Admin's "Mine / Everyone" choice on Searches. It lives outside the page so
// it survives a trip to the editor and back, and the editor's search
// switcher lists the same set.
export const showEveryone = ref(false)
