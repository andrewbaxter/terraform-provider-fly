---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "fly_ip Data Source - fly"
subcategory: ""
description: |-
  
---

# fly_ip (Data Source)





<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `app` (String) The App this resource will be created in
- `id` (String) A fly-generated ID

### Read-Only

- `address` (String) Empty if using `shared_v4`
- `region` (String) Fly region, ex `ord`, `sin`, `mad`
- `type` (String) `v4`, `v6`, or `private_v6`
